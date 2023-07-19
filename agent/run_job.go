package agent

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/hook"
	"github.com/buildkite/agent/v3/kubernetes"
	"github.com/buildkite/agent/v3/metrics"
	"github.com/buildkite/agent/v3/process"
	"github.com/buildkite/agent/v3/status"
)

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
	exit := &processExit{}

	// Used to wait on various routines that we spin up
	var wg sync.WaitGroup

	// Set up a child context for helper goroutines related to running the job.
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()

	defer r.cleanup(cctx, &wg, exit)

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

			exit.Status = "-1"
			exit.SignalReason = "agent_refused"

			return nil
		}
	}

	// Kick off log streaming and job status checking when the process starts.
	wg.Add(2)
	go r.jobLogStreamer(cctx, &wg)
	go r.jobCancellationChecker(cctx, &wg)

	exit = r.runJob(cctx) // Ignore gostaticcheck here, the value of exit is captured by the deferred cleanup function above

	return nil
}

type processExit struct {
	Status       string
	Signal       string
	SignalReason string
}

func (r *JobRunner) runJob(ctx context.Context) *processExit {
	exit := &processExit{}
	// Run the process. This will block until it finishes.
	if err := r.process.Run(ctx); err != nil {
		// Send the error as output
		r.logStreamer.Process([]byte(err.Error()))

		// The process did not run at all, so make sure it fails
		return &processExit{
			Status:       "-1",
			SignalReason: "process_run_error",
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
	exit.Status = fmt.Sprintf("%d", r.process.WaitStatus().ExitStatus())

	if ws := r.process.WaitStatus(); ws.Signaled() {
		exit.Signal = process.SignalString(ws.Signal())
	}

	switch {
	case r.stopped:
		// The agent is being gracefully stopped, and we signaled the job to end. Often due
		// to pending host shutdown or EC2 spot instance termination
		exit.SignalReason = "agent_stop"

	case r.cancelled:
		// The job was signaled because it was cancelled via the buildkite web UI
		exit.SignalReason = "cancel"
	}

	return exit
}

func (r *JobRunner) cleanup(ctx context.Context, wg *sync.WaitGroup, exit *processExit) {
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
	jobMetrics := r.conf.MetricsScope.With(metrics.Tags{"exit_code": exit.Status})

	if exit.Status == "0" {
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
