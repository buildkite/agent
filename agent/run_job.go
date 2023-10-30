package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/hook"
	"github.com/buildkite/agent/v3/kubernetes"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/metrics"
	"github.com/buildkite/agent/v3/process"
	"github.com/buildkite/agent/v3/status"
	"github.com/buildkite/roko"
)

const (
	SignalReasonAgentRefused      = "agent_refused"
	SignalReasonAgentStop         = "agent_stop"
	SignalReasonCancel            = "cancel"
	SignalReasonSignatureRejected = "signature_rejected"
	SignalReasonProcessRunError   = "process_run_error"
)

type missingKeyError struct {
	signature string
}

func (e *missingKeyError) Error() string {
	return fmt.Sprintf("job was signed with signature %q, but no verification key was provided", e.signature)
}

// Runs the job
func (r *JobRunner) Run(ctx context.Context) error {
	r.logger.Info("Starting job %s", r.conf.Job.ID)

	ctx, done := status.AddItem(ctx, "Job Runner", "", nil)
	defer done()

	r.startedAt = time.Now()

	// Start the build in the Buildkite Agent API. This is the first thing
	// we do so if it fails, we don't have to worry about cleaning things
	// up like started log streamer workers, and so on.
	if err := r.startJob(ctx, r.startedAt); err != nil {
		return err
	}

	// If this agent successfully grabs the job from the API, publish metric for
	// how long this job was in the queue for, if we can calculate that
	if r.conf.Job.RunnableAt != "" {
		runnableAt, err := time.Parse(time.RFC3339Nano, r.conf.Job.RunnableAt)
		if err != nil {
			r.logger.Error("Metric submission failed to parse %s", r.conf.Job.RunnableAt)
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
	exit := processExit{}

	// Used to wait on various routines that we spin up
	var wg sync.WaitGroup

	// Set up a child context for helper goroutines related to running the job.
	cctx, cancel := context.WithCancel(ctx)
	defer func(ctx context.Context, wg *sync.WaitGroup) {
		cancel()
		r.cleanup(ctx, wg, exit)
	}(ctx, &wg) // Note the non-cancellable context (ctx rather than cctx) here - we don't want to be interrupted during cleanup

	job := r.conf.Job

	if r.conf.JWKS == nil && job.Step.Signature != nil {
		r.verificationFailureLogs(
			VerificationBehaviourBlock,
			&missingKeyError{signature: job.Step.Signature.Value},
		)
		exit.Status = -1
		exit.SignalReason = SignalReasonSignatureRejected
		return nil
	}

	if r.conf.JWKS != nil {
		ise := &invalidSignatureError{}
		switch err := r.verifyJob(r.conf.JWKS); {
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
			l := r.logger.WithFields(logger.StringField("jobID", job.ID), logger.StringField("signature", job.Step.Signature.Value))
			l.Info("Successfully verified job")
			r.logStreamer.Process(r.prependTimestampForLogs("~~~ ✅ Job verified\n"))
			r.logStreamer.Process(r.prependTimestampForLogs("signature: %s\n", job.Step.Signature.Value))
		}
	}

	// Validate the repository if the list of allowed repositories is set.
	err := validateJobValue(r.conf.AgentConfiguration.AllowedRepositories, job.Env["BUILDKITE_REPO"])
	if err != nil {
		r.logStreamer.Process([]byte(fmt.Sprintf("%s", err)))
		r.logger.Error("Repo %s", err)
		exit.Status = -1
		exit.SignalReason = SignalReasonAgentRefused
		return nil
	}
	// Validate the plugins if the list of allowed plugins is set.
	err = r.checkPlugins(ctx)
	if err != nil {
		r.logStreamer.Process([]byte(fmt.Sprintf("%s", err)))
		r.logger.Error("Plugin %s", err)
		exit.Status = -1
		exit.SignalReason = SignalReasonAgentRefused
		return nil
	}

	// Before executing the bootstrap process with the received Job env, execute the pre-bootstrap hook (if present) for
	// it to tell us whether it is happy to proceed.
	if hook, _ := hook.Find(r.conf.AgentConfiguration.HooksPath, "pre-bootstrap"); hook != "" {
		// Once we have a hook any failure to run it MUST be fatal to the job to guarantee a true positive result from the hook
		ok, err := r.executePreBootstrapHook(ctx, hook)
		if !ok {
			// Ensure the Job UI knows why this job resulted in failure
			r.logStreamer.Process([]byte("pre-bootstrap hook rejected this job, see the buildkite-agent logs for more details"))
			// But disclose more information in the agent logs
			r.logger.Error("pre-bootstrap hook rejected this job: %s", err)

			exit.Status = -1
			exit.SignalReason = SignalReasonAgentRefused

			return nil
		}
	}

	// Kick off log streaming and job status checking when the process starts.
	wg.Add(2)
	go r.jobLogStreamer(cctx, &wg)
	go r.jobCancellationChecker(cctx, &wg)

	exit = r.runJob(cctx)

	return nil
}

func (r *JobRunner) prependTimestampForLogs(s string, args ...any) []byte {
	switch {
	case r.conf.AgentConfiguration.ANSITimestamps:
		return []byte(fmt.Sprintf(
			"\x1b_bk;t=%d\x07%s",
			time.Now().UnixNano()/int64(time.Millisecond),
			fmt.Sprintf(s, args...),
		))
	case r.conf.AgentConfiguration.TimestampLines:
		return []byte(fmt.Sprintf(
			"[%s] %s",
			time.Now().UTC().Format(time.RFC3339),
			fmt.Sprintf(s, args...),
		))
	default:
		return []byte(fmt.Sprintf(s, args...))
	}
}

func (r *JobRunner) verificationFailureLogs(behavior string, err error) {
	l := r.logger.WithFields(logger.StringField("jobID", r.conf.Job.ID), logger.StringField("error", err.Error()))
	prefix := "~~~ ⚠️"
	if behavior == VerificationBehaviourBlock {
		prefix = "+++ ⛔"
	}

	l.Warn("Job verification failed")
	r.logStreamer.Process([]byte(r.prependTimestampForLogs("%s Job verification failed\n", prefix)))
	r.logStreamer.Process([]byte(r.prependTimestampForLogs("error: %s\n", err)))

	if errors.Is(err, ErrNoSignature) {
		r.logStreamer.Process([]byte(r.prependTimestampForLogs("no signature in job\n")))
	} else if ise := new(invalidSignatureError); errors.As(err, &ise) {
		r.logStreamer.Process([]byte(r.prependTimestampForLogs("signature: %s\n", r.conf.Job.Step.Signature.Value)))
	} else if mke := new(missingKeyError); errors.As(err, &mke) {
		r.logStreamer.Process([]byte(r.prependTimestampForLogs("signature: %s\n", mke.signature)))
	}

	if behavior == VerificationBehaviourWarn {
		r.logger.Warn("Job will be run whether or not it can be verified - this is not recommended. You can change this behavior with the `job-verification-failure-behavior` agent configuration option.")
		r.logStreamer.Process(r.prependTimestampForLogs("Job will be run without verification\n"))
	}
}

type processExit struct {
	Status       int
	Signal       string
	SignalReason string
}

func (r *JobRunner) runJob(ctx context.Context) processExit {
	exit := processExit{}
	// Run the process. This will block until it finishes.
	if err := r.process.Run(ctx); err != nil {
		// Send the error as output
		r.logStreamer.Process([]byte(err.Error()))

		// The process did not run at all, so make sure it fails
		return processExit{
			Status:       -1,
			SignalReason: SignalReasonProcessRunError,
		}
	}
	// Intended to capture situations where the job-exec (aka bootstrap) container did not
	// start. Normally such errors are hidden in the Kubernetes events. Let's feed them up
	// to the user as they may be the caused by errors in the pipeline definition.
	k8sProcess, ok := r.process.(*kubernetes.Runner)
	if ok && r.cancelled && !r.stopped && k8sProcess.ClientStateUnknown() {
		r.logStreamer.Process([]byte(
			"Some containers had unknown exit statuses. Perhaps they were in ImagePullBackOff.",
		))
	}

	// Add the final output to the streamer
	r.logStreamer.Process(r.output.ReadAndTruncate())

	// Collect the finished process' exit status
	exit.Status = r.process.WaitStatus().ExitStatus()

	if ws := r.process.WaitStatus(); ws.Signaled() {
		exit.Signal = process.SignalString(ws.Signal())
	}

	switch {
	case r.stopped:
		// The agent is being gracefully stopped, and we signaled the job to end. Often due
		// to pending host shutdown or EC2 spot instance termination
		exit.SignalReason = SignalReasonAgentStop

	case r.cancelled:
		// The job was signaled because it was cancelled via the buildkite web UI
		exit.SignalReason = SignalReasonCancel
	}

	return exit
}

func (r *JobRunner) cleanup(ctx context.Context, wg *sync.WaitGroup, exit processExit) {
	finishedAt := time.Now()

	// Stop the header time streamer. This will block until all the chunks have been uploaded
	r.headerTimesStreamer.Stop()

	// Stop the log streamer. This will block until all the chunks have been uploaded
	r.logStreamer.Stop()

	// Warn about failed chunks
	if count := r.logStreamer.FailedChunks(); count > 0 {
		r.logger.Warn("%d chunks failed to upload for this job", count)
	}

	// Wait for the routines that we spun up to finish
	r.logger.Debug("[JobRunner] Waiting for all other routines to finish")
	wg.Wait()

	// Remove the env file, if any
	if r.envFile != nil {
		if err := os.Remove(r.envFile.Name()); err != nil {
			r.logger.Warn("[JobRunner] Error cleaning up env file: %s", err)
		}
		r.logger.Debug("[JobRunner] Deleted env file: %s", r.envFile.Name())
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
	r.finishJob(ctx, finishedAt, exit, r.logStreamer.FailedChunks())

	r.logger.Info("Finished job %s", r.conf.Job.ID)
}

// finishJob finishes the job in the Buildkite Agent API. If the FinishJob call
// cannot return successfully, this will retry for a long time.
func (r *JobRunner) finishJob(ctx context.Context, finishedAt time.Time, exit processExit, failedChunkCount int) error {
	r.conf.Job.FinishedAt = finishedAt.UTC().Format(time.RFC3339Nano)
	r.conf.Job.ExitStatus = strconv.Itoa(exit.Status)
	r.conf.Job.Signal = exit.Signal
	r.conf.Job.SignalReason = exit.SignalReason
	r.conf.Job.ChunksFailedCount = failedChunkCount

	r.logger.Debug("[JobRunner] Finishing job with exit_status=%s, signal=%s and signal_reason=%s",
		r.conf.Job.ExitStatus, r.conf.Job.Signal, r.conf.Job.SignalReason)

	ctx, cancel := context.WithTimeout(ctx, 48*time.Hour)
	defer cancel()

	return roko.NewRetrier(
		roko.TryForever(),
		roko.WithJitter(),
		roko.WithStrategy(roko.Constant(1*time.Second)),
	).DoWithContext(ctx, func(retrier *roko.Retrier) error {
		response, err := r.apiClient.FinishJob(ctx, r.conf.Job)
		if err != nil {
			// If the API returns with a 422, that means that we
			// successfully tried to finish the job, but Buildkite
			// rejected the finish for some reason. This can
			// sometimes mean that Buildkite has cancelled the job
			// before we get a chance to send the final API call
			// (maybe this agent took too long to kill the
			// process). In that case, we don't want to keep trying
			// to finish the job forever so we'll just bail out and
			// go find some more work to do.
			if response != nil && response.StatusCode == 422 {
				r.logger.Warn("Buildkite rejected the call to finish the job (%s)", err)
				retrier.Break()
			} else {
				r.logger.Warn("%s (%s)", err, retrier)
			}
		}

		return err
	})
}

// jobLogStreamer waits for the process to start, then grabs the job output
// every few seconds and sends it back to Buildkite.
func (r *JobRunner) jobLogStreamer(ctx context.Context, wg *sync.WaitGroup) {
	ctx, setStat, done := status.AddSimpleItem(ctx, "Job Log Streamer")
	defer done()
	setStat("🏃 Starting...")

	defer func() {
		wg.Done()
		r.logger.Debug("[JobRunner] Routine that processes the log has finished")
	}()

	select {
	case <-r.process.Started():
	case <-ctx.Done():
		return
	}

	for {
		setStat("📨 Sending process output to log streamer")

		// Send the output of the process to the log streamer
		// for processing
		chunk := r.output.ReadAndTruncate()
		if err := r.logStreamer.Process(chunk); err != nil {
			r.logger.Error("Could not stream the log output: %v", err)
			// So far, the only error from logStreamer.Process is if the log has
			// reached the limit.
			// Since we're not writing any more, close the buffer, which will
			// cause future Writes to return an error.
			r.output.Close()
		}

		setStat("😴 Sleeping for a bit")

		// Sleep for a bit, or until the job is finished
		select {
		case <-time.After(1 * time.Second):
		case <-ctx.Done():
			return
		case <-r.process.Done():
			return
		}
	}

	// The final output after the process has finished is processed in Run().
}
func (r *JobRunner) CancelAndStop() error {
	r.cancelLock.Lock()
	r.stopped = true
	r.cancelLock.Unlock()
	return r.Cancel()
}

func (r *JobRunner) Cancel() error {
	r.cancelLock.Lock()
	defer r.cancelLock.Unlock()

	if r.cancelled {
		return nil
	}

	if r.process == nil {
		r.logger.Error("No process to kill")
		return nil
	}

	reason := ""
	if r.stopped {
		reason = "(agent stopping)"
	}

	r.logger.Info("Canceling job %s with a grace period of %ds %s", r.conf.Job.ID, r.conf.AgentConfiguration.CancelGracePeriod, reason)

	r.cancelled = true

	// First we interrupt the process (ctrl-c or SIGINT)
	if err := r.process.Interrupt(); err != nil {
		return err
	}

	select {
	// Grace period for cancelling
	case <-time.After(time.Second * time.Duration(r.conf.AgentConfiguration.CancelGracePeriod)):
		r.logger.Info("Job %s hasn't stopped in time, terminating", r.conf.Job.ID)

		// Terminate the process as we've exceeded our context
		return r.process.Terminate()

	// Process successfully terminated
	case <-r.process.Done():
		return nil
	}
}
