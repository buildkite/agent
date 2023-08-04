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
	"github.com/buildkite/agent/v3/internal/pipeline"
	"github.com/buildkite/agent/v3/kubernetes"
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

// Runs the job
func (r *JobRunner) Run(ctx context.Context) error {
	r.logger.Info("Starting job %s", r.conf.Job.ID)

	ctx, done := status.AddItem(ctx, "Job Runner", "", nil)
	defer done()

	r.startedAt = time.Now()

	var verifier pipeline.Verifier
	if r.conf.AgentConfiguration.JobVerificationKeyPath != "" {
		verificationKey, err := os.ReadFile(r.conf.AgentConfiguration.JobVerificationKeyPath)
		if err != nil {
			return fmt.Errorf("failed to read job verification key: %w", err)
		}

		verifier, err = pipeline.NewVerifier("hmac-sha256", verificationKey)
		if err != nil {
			return fmt.Errorf("failed to create job verifier: %w", err)
		}
	}

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

	if verifier == nil && job.Step.Signature != nil {
		r.verificationFailureLogs(
			fmt.Errorf("job %q was signed with signature %q, but no verification key was provided, so the job can't be verified", job.ID, job.Step.Signature.Value),
			VerificationBehaviourBlock,
		)
		exit.Status = -1
		exit.SignalReason = SignalReasonSignatureRejected
		return nil
	}

	if verifier != nil {
		ise := &invalidSignatureError{}
		switch err := r.verifyJob(verifier); {
		case errors.Is(err, ErrNoSignature):
			r.verificationFailureLogs(err, r.NoSignatureBehavior)
			if r.NoSignatureBehavior == VerificationBehaviourBlock {
				exit.Status = -1
				exit.SignalReason = SignalReasonSignatureRejected
				return nil
			}

		case errors.As(err, &ise):
			r.verificationFailureLogs(err, r.InvalidSignatureBehavior)
			if r.InvalidSignatureBehavior == VerificationBehaviourBlock {
				exit.Status = -1
				exit.SignalReason = SignalReasonSignatureRejected
				return nil
			}

		case err != nil: // some other error
			r.verificationFailureLogs(err, VerificationBehaviourBlock) // errors in verification are always fatal
			exit.Status = -1
			exit.SignalReason = SignalReasonSignatureRejected
			return nil

		default: // no error, all good, keep going
			r.logger.Info("Successfully verified job %s with signature %s", job.ID, job.Step.Signature.Value)
			r.logStreamer.Process([]byte(fmt.Sprintf("✅ Verified job with signature %s\n", job.Step.Signature.Value)))
		}
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
			// succesfully tried to finish the job, but Buildkite
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
