package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/core"
	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/internal/job/hook"
	"github.com/buildkite/agent/v3/kubernetes"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/metrics"
	"github.com/buildkite/agent/v3/process"
	"github.com/buildkite/agent/v3/status"
	"github.com/buildkite/go-pipeline"
)

const (
	// Think very carefully about adding new signal reasons. If you must add a new signal reason, it must also be added to
	// the Job::Event::SignalReason enum in the rails app.
	//
	// They are meant to represent the reason a job was stopped, but they've also been used to
	// represent the reason a job wasn't started at all. This is fine but we don't want to pile more
	// on as customers catch these signal reasons when configuring retry attempts. When we add more
	// signal reasons we force customers to update their retry configurations to catch the new signal
	// reasons.
	//
	// We should consider adding new fields 'not_run_reason' and 'not_run_details' instead of adding
	// more signal reasons.

	// SignalReasonAgentRefused is used when the agent refused to run the job, e.g. due to a failed `pre-bootstrap` hook,
	// or a failed allowlist validation.
	SignalReasonAgentRefused = "agent_refused"

	// SignalReasonAgentStop is used when the agent is stopping, e.g. due to a pending host shutdown or EC2 spot instance termination.
	SignalReasonAgentStop = "agent_stop"

	// SignalReasonCancel is used when the job was cancelled via the Buildkite web UI.
	SignalReasonCancel = "cancel"

	// SignalReasonSignatureRejected is used when the job was signed with a signature that could not be verified, either
	// because the signature is invalid, or because the agent does not have the verification key.
	SignalReasonSignatureRejected = "signature_rejected"

	// SignalReasonProcessRunError is used when the process to run the bootstrap script failed to run, e.g. due to a
	// missing executable, or a permission error
	SignalReasonProcessRunError = "process_run_error"

	// SignalReasonStackError is used when the job was stopped due to a stack error, eg because in Kubernetes the pod running
	// could not be launched. This signal reason is not used directly by the agent, but is used by the agent-stack-kubernetes
	// to signal that the job was not run due to a stack error.
	SignalReasonStackError = "stack_error"
)

type missingKeyError struct {
	signature string
}

func (e *missingKeyError) Error() string {
	return fmt.Sprintf("job was signed with signature %q, but no verification key was provided", e.signature)
}

// Run runs the job.
func (r *JobRunner) Run(ctx context.Context, ignoreAgentInDispatches *bool) (err error) {
	if r.cancelled.Load() {
		return errors.New("job already cancelled before running")
	}

	r.agentLogger.Info("Starting job %s for build at %s", r.conf.Job.ID, r.conf.Job.Env["BUILDKITE_BUILD_URL"])

	ctx, done := status.AddItem(ctx, "Job Runner", "", nil)
	defer done()

	r.startedAt = time.Now()

	// Start the build in the Buildkite Agent API. This is the first thing
	// we do so if it fails, we don't have to worry about cleaning things
	// up like started log streamer workers, and so on.
	if err := r.client.StartJob(ctx, r.conf.Job, r.startedAt); err != nil {
		return err
	}

	// If this agent successfully grabs the job from the API, publish metric for
	// how long this job was in the queue for, if we can calculate that
	if r.conf.Job.RunnableAt != "" {
		runnableAt, err := time.Parse(time.RFC3339Nano, r.conf.Job.RunnableAt)
		if err != nil {
			r.agentLogger.Error("Metric submission failed to parse %s", r.conf.Job.RunnableAt)
		} else {
			r.conf.MetricsScope.Timing("queue.duration", r.startedAt.Sub(runnableAt))
		}
	}

	// Start the header time streamer
	go r.headerTimesStreamer.Run(ctx)

	// Start the log streamer. Launches multiple goroutines.
	if err := r.logStreamer.Start(ctx); err != nil {
		return err
	}

	// Default exit status is no exit status
	exit := core.ProcessExit{}

	// Used to wait on various routines that we spin up
	var wg sync.WaitGroup

	// Set up a child context for helper goroutines related to running the job.
	cctx, cancel := context.WithCancel(ctx)
	defer func(ctx context.Context, wg *sync.WaitGroup) {
		cancel()
		r.cleanup(ctx, wg, exit, ignoreAgentInDispatches)

		// In acquire-job mode, we want to return any failed exit code so that
		// the program can exit with the same code.
		if r.conf.AgentConfiguration.AcquireJob != "" && exit.Status != 0 {
			// Use errors.Join to return both this exit, and whatever err may
			// be being returned already.
			err = errors.Join(exit, err)
		}
	}(ctx, &wg) // Note the non-cancellable context (ctx rather than cctx) here - we don't want to be interrupted during cleanup

	job := r.conf.Job

	if r.conf.JWKS == nil && job.Step.Signature != nil {
		r.verificationFailureLogs(
			VerificationBehaviourBlock,
			fmt.Errorf("cannot verify signature. JWK for pipeline verification is not configured"),
		)

		if r.VerificationFailureBehavior == VerificationBehaviourBlock {
			exit.Status = -1
			exit.SignalReason = SignalReasonSignatureRejected
			return nil
		}
	}

	if r.conf.JWKS != nil {
		ise := &invalidSignatureError{}
		switch err := r.verifyJob(ctx, r.conf.JWKS); {
		case errors.Is(err, ErrNoSignature) || errors.As(err, &ise):
			r.verificationFailureLogs(r.VerificationFailureBehavior, err)
			if r.VerificationFailureBehavior == VerificationBehaviourBlock {
				exit.Status = -1
				exit.SignalReason = SignalReasonSignatureRejected
				return nil
			}

		case err != nil: // some other error
			r.verificationFailureLogs(VerificationBehaviourBlock, err) // errors in verification are always fatal
			exit.Status = -1
			exit.SignalReason = SignalReasonSignatureRejected
			return nil

		default: // no error, all good, keep going
			l := r.agentLogger.WithFields(logger.StringField("jobID", job.ID), logger.StringField("signature", job.Step.Signature.Value))
			l.Info("Successfully verified job")
			fmt.Fprintln(r.jobLogs, "~~~ ✅ Job signature verified") //nolint:errcheck // job log write; errors are non-actionable
			fmt.Fprintf(r.jobLogs, "signature: %s\n", job.Step.Signature.Value) //nolint:errcheck // job log write; errors are non-actionable
		}
	}

	// Validate the repository if the list of allowed repositories is set.
	if err := r.validateConfigAllowlists(job); err != nil {
		fmt.Fprintln(r.jobLogs, err.Error()) //nolint:errcheck // job log write; errors are non-actionable
		r.agentLogger.Error(err.Error())

		exit.Status = -1
		exit.SignalReason = SignalReasonAgentRefused
		return nil
	}

	// Before executing the bootstrap process with the received Job env, execute the pre-bootstrap hook (if present) for
	// it to tell us whether it is happy to proceed.
	if hook, _ := hook.Find(nil, r.conf.AgentConfiguration.HooksPath, "pre-bootstrap"); hook != "" {
		// Once we have a hook any failure to run it MUST be fatal to the job to guarantee a true positive result from the hook
		ok, err := r.executePreBootstrapHook(ctx, hook)
		if !ok {
			// Ensure the Job UI knows why this job resulted in failure
			fmt.Fprintln(r.jobLogs, "pre-bootstrap hook rejected this job, see the buildkite-agent logs for more details") //nolint:errcheck // job log write; errors are non-actionable
			// But disclose more information in the agent logs
			r.agentLogger.Error("pre-bootstrap hook rejected this job: %s", err)

			exit.Status = -1
			exit.SignalReason = SignalReasonAgentRefused

			return nil
		}
	}

	// Kick off log streaming and job status checking when the process starts.
	wg.Add(2)
	go r.streamJobLogsAfterProcessStart(cctx, &wg)
	go r.jobCancellationChecker(cctx, &wg)

	exit = r.runJob(cctx)
	// The defer mutates the error return in some cases.
	return nil
}

func (r *JobRunner) validateConfigAllowlists(job *api.Job) error {
	validations := map[string]func() error{
		"repo": func() error {
			return validateJobValue(r.conf.AgentConfiguration.AllowedRepositories, job.Env["BUILDKITE_REPO"])
		},
		"environment variables": func() error {
			return validateEnv(job.Env, r.conf.AgentConfiguration.AllowedEnvironmentVariables)
		},
		"plugins": r.validatePlugins,
	}

	for name, validation := range validations {
		if err := validation(); err != nil {
			return fmt.Errorf("failed to validate %s: %w", name, err)
		}
	}

	return nil
}

// validateEnv checks if the environment variables are allowed by ensuring their names match at least one of the regular
// expressions in the allowedVariablePatterns list. The error message will contain all of the variables that aren't are
// invalid.
func validateEnv(env map[string]string, allowedVariablePatterns []*regexp.Regexp) error {
	if len(allowedVariablePatterns) == 0 {
		return nil
	}

	for k := range env {
		if err := validateJobValue(allowedVariablePatterns, k); err != nil {
			return err
		}
	}

	return nil
}

// validateJobValue returns an error if a job value doesn't match
// any allowed patterns.
func validateJobValue(allowedPatterns []*regexp.Regexp, jobValue string) error {
	if len(allowedPatterns) == 0 {
		return nil
	}

	for _, re := range allowedPatterns {
		if match := re.MatchString(jobValue); match {
			return nil
		}
	}

	return fmt.Errorf("%s has no match in %s", jobValue, allowedPatterns)
}

// validatePlugins unmarshal and validates the plugins, if the list of allowed plugins is set.
// Disabled plugins or errors in json.Unmarshal will by-pass the plugin verification.
func (r *JobRunner) validatePlugins() error {
	if !r.conf.AgentConfiguration.PluginsEnabled {
		return nil
	}

	pluginsVar := []byte(r.conf.Job.Env["BUILDKITE_PLUGINS"])
	if len(pluginsVar) == 0 {
		return nil
	}

	var ps pipeline.Plugins
	if err := json.Unmarshal(pluginsVar, &ps); err != nil {
		return fmt.Errorf("failed to unmarshal plugins for validation: %w", err)
	}

	for _, plugin := range ps {
		if err := validateJobValue(r.conf.AgentConfiguration.AllowedPlugins, plugin.FullSource()); err != nil {
			return err
		}
	}

	return nil
}

func (r *JobRunner) verificationFailureLogs(behavior string, err error) {
	l := r.agentLogger.WithFields(
		logger.StringField("jobID", r.conf.Job.ID),
		logger.StringField("error", err.Error()),
	)
	prefix := "+++ ⚠️"
	if behavior == VerificationBehaviourBlock {
		prefix = "+++ ⛔"
	}

	l.Warn("Job verification failed")
	fmt.Fprintf(r.jobLogs, "%s Job signature verification failed\n", prefix)  //nolint:errcheck // job log write; errors are non-actionable
	fmt.Fprintf(r.jobLogs, "error: %s\n", err)                               //nolint:errcheck // job log write; errors are non-actionable

	if errors.Is(err, ErrNoSignature) {
		fmt.Fprintln(r.jobLogs, "no signature in job") //nolint:errcheck // job log write; errors are non-actionable
	} else if ise := new(invalidSignatureError); errors.As(err, &ise) {
		fmt.Fprintf(r.jobLogs, "signature: %s\n", r.conf.Job.Step.Signature.Value) //nolint:errcheck // job log write; errors are non-actionable
	} else if mke := new(missingKeyError); errors.As(err, &mke) {
		fmt.Fprintf(r.jobLogs, "signature: %s\n", mke.signature) //nolint:errcheck // job log write; errors are non-actionable
	}

	if behavior == VerificationBehaviourWarn {
		l.Warn("Job will be run whether or not it can be verified - this is not recommended.")
		l.Warn("You can change this behavior with the `verification-failure-behavior` agent configuration option.")
		fmt.Fprintln(r.jobLogs, "Job will be run without verification") //nolint:errcheck // job log write; errors are non-actionable
	}
}

func (r *JobRunner) runJob(ctx context.Context) core.ProcessExit {
	exit := core.ProcessExit{}
	// Run the process. This will block until it finishes.
	if err := r.process.Run(ctx); err != nil {
		// Send the error to job logs
		fmt.Fprintf(r.jobLogs, "Error running job: %s\n", err) //nolint:errcheck // job log write; errors are non-actionable

		// The process did not run at all, so make sure it fails
		return core.ProcessExit{
			Status:       -1,
			SignalReason: SignalReasonProcessRunError,
		}
	}
	// Intended to capture situations where the job-exec (aka bootstrap) container did not
	// start. Normally such errors are hidden in the Kubernetes events. Let's feed them up
	// to the user as they may be the caused by errors in the pipeline definition.
	k8sProcess, isK8s := r.process.(*kubernetes.Runner)
	if isK8s && !r.agentStopping.Load() {
		switch {
		case r.cancelled.Load() && k8sProcess.AnyClientIn(kubernetes.StateNotYetConnected):
			//nolint:errcheck // job log write; errors are non-actionable
			fmt.Fprint(r.jobLogs, `+++ Unknown container exit status
One or more containers never connected to the agent. Perhaps the container image specified in your podSpec could not be pulled (ImagePullBackOff)?
`)
		case k8sProcess.AnyClientIn(kubernetes.StateLost):
			//nolint:errcheck // job log write; errors are non-actionable
			fmt.Fprint(r.jobLogs, `+++ Unknown container exit status
One or more containers connected to the agent, but then stopped communicating without exiting normally. Perhaps the container was OOM-killed?
`)
		}
	}

	// Collect the finished process' exit status
	exit.Status = r.process.WaitStatus().ExitStatus()

	if ws := r.process.WaitStatus(); ws.Signaled() {
		exit.Signal = process.SignalString(ws.Signal())
	}

	switch {
	case r.agentStopping.Load():
		// The agent is being ungracefully stopped, and we signaled the job to
		// end. Often due to pending host shutdown or EC2 spot instance termination
		exit.SignalReason = SignalReasonAgentStop

	case r.cancelled.Load():
		// The job was signaled because it was cancelled via the buildkite web UI
		exit.SignalReason = SignalReasonCancel

		if exit.Status == 0 {
			// On Windows, a signalled process exits 0 rather than non-zero.
			// This is inconsistent with cancellation on other platforms.
			if experiments.IsEnabled(ctx, experiments.OverrideZeroExitOnCancel) {
				exit.Status = 1
			}
		}
	}

	return exit
}

func (r *JobRunner) cleanup(ctx context.Context, wg *sync.WaitGroup, exit core.ProcessExit, ignoreAgentInDispatches *bool) {
	finishedAt := time.Now()

	// Flush the job logs. If the process is never started, then logs from prior to the attempt to
	// start the process will still be buffered. Also, there may still be logs in the buffer that
	// were left behind because the uploader goroutine exited before it could flush them.
	if err := r.logStreamer.Process(ctx, r.output.ReadAndTruncate()); err != nil {
		r.agentLogger.Warn("Log streamer couldn't process final logs: %v", err)
	}

	// Stop the log streamer. This will block until all the chunks have been uploaded
	r.logStreamer.Stop()

	// Stop the header time streamer. This will block until all the chunks have been uploaded
	r.headerTimesStreamer.Stop()

	// Warn about failed chunks
	if count := r.logStreamer.FailedChunks(); count > 0 {
		r.agentLogger.Warn("%d chunks failed to upload for this job", count)
	}

	// Wait for the routines that we spun up to finish
	r.agentLogger.Debug("[JobRunner] Waiting for all other routines to finish")
	wg.Wait()

	// Remove the env file, if any
	for _, f := range []*os.File{r.envShellFile, r.envJSONFile} {
		if f == nil {
			continue
		}
		if err := os.Remove(f.Name()); err != nil {
			r.agentLogger.Warn("[JobRunner] Error cleaning up env file: %s", err)
			continue
		}
		r.agentLogger.Debug("[JobRunner] Deleted env file: %s", f.Name())
	}

	// Write some metrics about the job run
	jobMetrics := r.conf.MetricsScope.With(metrics.Tags{"exit_code": strconv.Itoa(exit.Status)})

	if exit.Status == 0 {
		jobMetrics.Timing("jobs.duration.success", finishedAt.Sub(r.startedAt))
		jobMetrics.Count("jobs.success", 1)
	} else {
		jobMetrics.Timing("jobs.duration.error", finishedAt.Sub(r.startedAt))
		jobMetrics.Count("jobs.failed", 1)
	}

	// Finish the build in the Buildkite Agent API
	// Once we tell the API we're finished it might assign us new work, so make sure everything else is done first.
	if err := r.client.FinishJob(ctx, r.conf.Job, finishedAt, exit, r.logStreamer.FailedChunks(), ignoreAgentInDispatches); err != nil {
		r.agentLogger.Error("Couldn't mark job as finished: %v", err)
	}

	r.agentLogger.Info("Finished job %s for build at %s", r.conf.Job.ID, r.conf.Job.Env["BUILDKITE_BUILD_URL"])
}

// streamJobLogsAfterProcessStart waits for the process to start, then grabs the job output
// every few seconds and sends it back to Buildkite.
func (r *JobRunner) streamJobLogsAfterProcessStart(ctx context.Context, wg *sync.WaitGroup) {
	ctx, setStat, done := status.AddSimpleItem(ctx, "Job Log Streamer")
	defer done()
	setStat("🏃 Starting...")

	defer func() {
		wg.Done()
		r.agentLogger.Debug("[JobRunner] Routine that processes the log has finished")
	}()

	select {
	case <-r.process.Started():
	case <-ctx.Done():
		return
	}

	processInterval := 1 * time.Second
	if r.conf.Job.ChunksIntervalSeconds > 0 {
		processInterval = time.Duration(r.conf.Job.ChunksIntervalSeconds) * time.Second
	}

	// We want log chunks to be uploaded regularly. If there were only a few
	// agents in the world, a simple ticker loop would be fine. But there are a
	// lot of agents, and we want to spread the requests over time. Some things
	// to consider when designing such a loop:
	//
	// - Large groups of agents all starting the loop at the same time
	// - Clock drift or backend issues causing the loops among agents to start
	//   synchronising
	// - Statistical reasons for agents tending to synchronise
	// - Having loose or tight bounds on the time between requests
	//
	// In this case we want both a lower bound, because fewer larger chunks are
	// more efficient than lots of smaller chunks, but also we probably want an
	// upper bound, to improve the experience of tailing a log in the UI.
	//
	// Below is a loop within a loop. The inner loop is just a plain ticker loop
	// that processes chunks once per interval pretty much exactly.
	// The outer loop periodically restarts the inner loop at a random offset
	// in time (jitter).
	// Periodically applying jitter prevents large numbers of agents from
	// synchronising, but only doing so every so often makes the time between
	// requests regular (most of the time).
	//
	// 32 intervals is an arbitrarily chosen amount of time between jittering.
	// Increasing it will make it more susceptible to spontaneous
	// synchronsiation problems like clock drift,
	// decreasing it will add more variability but also add more "large gaps"
	// at the point where jitter is added.
	//
	// The outer loop repeats every 33 intervals, in order to fit both the inner
	// loop (32 intervals) and the jitter (between 0 and 1 interval):
	//   32 intervals < (jitter + inner loop) < 33 intervals
	// So the gap between requests will usually be 1 interval, but occasionally
	// be longer (up to 2 intervals).

	const runLength = 32
	rejitterTicker := time.Tick((runLength + 1) * processInterval)
	for {
		// The inner loop starts at a random offset within an interval.
		jitter := rand.N(processInterval)
		setStat(fmt.Sprintf("🫨 Jittering for %v", jitter))
		select {
		case <-time.After(jitter):
			// continue below
		case <-ctx.Done():
			return
		case <-r.process.Done():
			return
		}

		// The inner loop processes once per interval pretty much exactly.
		intervalTicker := time.Tick(processInterval)
		for range runLength {
			setStat("📨 Sending process output to log streamer")

			// Send the output of the process to the log streamer for processing
			if err := r.logStreamer.Process(ctx, r.output.ReadAndTruncate()); err != nil {
				r.agentLogger.Error("Could not stream the log output: %v", err)
				// LogStreamer.Process only returns an error when it can no longer
				// accept logs (maybe Stop was called, or a hard limit was reached).
				// Since we can no longer send logs, Close the buffer, which causes
				// future Writes to return io.ErrClosedPipe, typically SIGPIPE-ing
				// the running process (if it is still running).
				if err := r.output.Close(); err != nil && err != process.ErrAlreadyClosed {
					r.agentLogger.Error("Process output buffer could not be closed: %v", err)
				}
				return
			}

			setStat("😴 Waiting for next log processing interval tick")
			select {
			case <-intervalTicker:
				// continue next loop iteration
			case <-ctx.Done():
				return
			case <-r.process.Done():
				return
			}
		}

		// Start the next run on a fixed schedule (for statistical reasons).
		setStat("😴 Waiting for next re-jittering interval tick")
		select {
		case <-rejitterTicker:
			// continue next loop iteration
		case <-ctx.Done():
			return
		case <-r.process.Done():
			return
		}
	}

	// The final output after the process has finished is processed in Run().
}

// Cancel cancels the job. It can be summarised as:
//   - Send the process an Interrupt. When run via a subprocess, this translates
//     into SIGTERM. When run via the k8s socket, this transitions the connected
//     client to RunStateInterrupt.
//   - Wait for the signal grace period.
//   - If the job hasn't exited, send the process a Terminate. This is either
//     SIGKILL or closing the k8s socket server.
//
// Cancel blocks until this process is complete.
// The `agentStopping` arg mainly affects logged messages.
func (r *JobRunner) Cancel(reason CancelReason) error {
	r.cancelLock.Lock()
	defer r.cancelLock.Unlock()

	// In case the user clicks "Cancel" in the UI while the agent happens to be
	// stopping, only go from !stopping -> stopping.
	r.agentStopping.Store(r.agentStopping.Load() || reason == CancelReasonAgentStopping)

	// Return early if already cancelled.
	if !r.cancelled.CompareAndSwap(false, true) {
		return nil
	}

	if r.process == nil {
		r.agentLogger.Error("No process to kill")
		return nil
	}

	r.agentLogger.Info(
		"Canceling job %s with a signal grace period of %v (%s)",
		r.conf.Job.ID,
		r.conf.AgentConfiguration.SignalGracePeriod,
		reason,
	)

	// First we interrupt the process with the configured CancelSignal.
	// At some point in the past, for subprocesses, the default was intended to
	// be SIGINT, but you will find that the cancel-signal flag default and
	// the process package's default are both SIGTERM.
	if err := r.process.Interrupt(); err != nil {
		return err
	}

	select {
	// Grace period between Interrupt and Terminate = the signal grace period.
	// Extra time between the end of the signal grace period and the end of the
	// cancel grace period is the time we (agent side) need to upload logs and
	// disconnect (if the agent is exiting).
	case <-time.After(r.conf.AgentConfiguration.SignalGracePeriod):
		r.agentLogger.Info(
			"Job %s hasn't stopped within %v, terminating",
			r.conf.Job.ID,
			r.conf.AgentConfiguration.SignalGracePeriod,
		)

		// Terminate the process as we've exceeded our context
		return r.process.Terminate()

	// Process successfully terminated
	case <-r.process.Done():
		return nil
	}
}

// CancelReason captures the reason why Cancel is called.
type CancelReason int

const (
	CancelReasonJobState CancelReason = iota
	CancelReasonAgentStopping
	CancelReasonInvalidToken
)

func (r CancelReason) String() string {
	switch r {
	case CancelReasonJobState:
		return "job cancelled on Buildkite"
	case CancelReasonAgentStopping:
		return "agent is stopping"
	case CancelReasonInvalidToken:
		return "access token is invalid"
	}
	return "unknown"
}
