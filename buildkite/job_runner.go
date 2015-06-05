package buildkite

import (
	"fmt"
	"github.com/buildkite/agent/buildkite/logstreamer"
	"github.com/buildkite/agent/logger"
	process2 "github.com/buildkite/agent/process"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

type JobRunner struct {
	// The agent running the job
	Agent *Agent

	// The job being run
	Job *Job

	// The boostrap script to run
	BootstrapScript string

	// The path to the run the builds in
	BuildPath string

	// Where bootstrap hooks are found
	HooksPath string

	// If this agent is allowed to perform command evaluation
	CommandEval bool

	// Whether or not the agent is allowed to automatically accept SSH
	// fingerprints
	AutoSSHFingerprintVerification bool

	// Run jobs in a PTY
	RunInPty bool

	// The interal process of the job
	process *process2.Process

	// If the job is being cancelled
	cancelled bool
}

func (r *JobRunner) Kill() error {
	if r.cancelled {
		// Already canceled
	} else {
		logger.Info("Canceling job %s", r.Job.ID)
		r.cancelled = true

		if r.process != nil {
			r.process.Kill()
		} else {
			logger.Error("No process to kill")
		}
	}

	return nil
}

func (r *JobRunner) Run() error {
	logger.Info("Starting job %s", r.Job.ID)

	// Create a clone of our jobs environment. We'll then set the
	// environment variables provided by the agent, which will override any
	// sent by Buildkite. The variables below should always take
	// precedence.
	env := make(map[string]string)
	for key, value := range r.Job.Env {
		env[key] = value
	}

	// Add agent environment variables
	env["BUILDKITE_AGENT_ENDPOINT"] = r.Job.API.Endpoint
	env["BUILDKITE_AGENT_ACCESS_TOKEN"] = r.Job.API.Token
	env["BUILDKITE_AGENT_VERSION"] = Version()
	env["BUILDKITE_AGENT_DEBUG"] = fmt.Sprintf("%t", logger.GetLevel() == logger.DEBUG)

	// We know the BUILDKITE_BIN_PATH dir, because it's the path to the
	// currently running file (there is only 1 binary)
	dir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	env["BUILDKITE_BIN_PATH"] = dir

	// Add misc options
	env["BUILDKITE_BUILD_PATH"] = r.BuildPath
	env["BUILDKITE_HOOKS_PATH"] = r.HooksPath
	env["BUILDKITE_AUTO_SSH_FINGERPRINT_VERIFICATION"] = fmt.Sprintf("%t", r.AutoSSHFingerprintVerification)
	env["BUILDKITE_COMMAND_EVAL"] = fmt.Sprintf("%t", r.CommandEval)

	// Convert the env map into a slice (which is what the script gear
	// needs)
	envSlice := []string{}
	for key, value := range env {
		envSlice = append(envSlice, fmt.Sprintf("%s=%s", key, value))
	}

	// The HTTP request we'll be sending it to
	logStreamerRequest := r.Job.API.NewRequest("POST", "jobs/"+r.Job.ID+"/chunks", 10)

	// Create and start our log streamer
	logStreamer, err := logstreamer.New(logStreamerRequest, r.Job.ChunksMaxSizeBytes)
	if err != nil {
		logger.Error("%s", err)
	}

	logStreamer.Start()

	// Create our header times struct
	headerTimes := HeaderTimes{Job: r.Job, API: r.Job.API}

	// This callback is called when the process starts
	startCallback := func(process *process2.Process) {
		// Start a routine that will grab the output every few seconds and send it back to Buildkite
		go func() {
			for process.Running {
				// Send the output of the process to the log streamer for processing
				logStreamer.Process(process.Output())

				// Check for cancellations every second
				time.Sleep(1 * time.Second)
			}

			logger.Debug("Routine that processes the log has finished")
		}()

		// Start a routine that will grab the output every few seconds and send it back to Buildkite
		go func() {
			for process.Running {
				// Get the latest job status so we can see if
				// the job has been canceled
				err := r.Job.Refresh()
				if err != nil {
					// We don't really care if it fails,
					// we'll just try again in a second
					// anyway
					logger.Warn("Problem with getting job status %s (%s)", r.Job.ID, err)
				} else if r.Job.State == "canceled" {
					r.Kill()
				}

				// Check for cancellations every few seconds
				time.Sleep(3 * time.Second)
			}

			logger.Debug("Routine that refreshes the job has finished")
		}()
	}

	// The regular expression used to match headers
	headerRegexp, err := regexp.Compile("^(?:---|\\+\\+\\+|~~~)\\s(.+)?$")
	if err != nil {
		logger.Error("Failed to compile header regular expression (%T: %v)", err, err)
	}

	// This callback is called for every line that is output by the process
	lineCallback := func(process *process2.Process, line string) {
		// We'll also ignore any line over 500 characters (who has a
		// 500 character header...honestly...)
		if len(line) < 500 && headerRegexp.MatchString(line) {
			// logger.Debug("Found header \"%s\", capturing current time", line)
			go headerTimes.Now(line)
		}
	}

	// Initialize our process to run
	process := process2.InitProcess(r.BootstrapScript, envSlice, r.RunInPty, startCallback, lineCallback)

	// Store the process so we can cancel it later.
	r.process = process

	// Mark the build as started
	r.Job.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)

	// Start the build in the API
	err = r.Job.Start()
	if err != nil {
		return fmt.Errorf("Failed to start job: %s", err)
	}

	// If the job's state isn't `started` in the API, we should probably
	// bail because something's gone wrong.
	if r.Job.State != "running" {
		return fmt.Errorf("After starting the job, the state returned from the API was: `%s` but it needed to be: `running`", r.Job.State)
	}

	// Start the process. This will block until it finishes.
	err = process.Start()
	if err == nil {
		// Add the final output to the streamer
		logStreamer.Process(process.Output())
	} else {
		// Send the error as output
		logStreamer.Process(fmt.Sprintf("%s", err))
	}

	// Mark the build as finished
	r.Job.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	r.Job.ExitStatus = r.process.ExitStatus

	// Wait until all the header times have finished uploading
	headerTimes.Wait()

	// Stop the log streamer
	logStreamer.Stop()

	// Save how many chunks failed to upload
	r.Job.ChunksFailedCount = int(logStreamer.ChunksFailedCount)

	if r.Job.ChunksFailedCount > 0 {
		logger.Warn("%d chunks failed to upload for this job", r.Job.ChunksFailedCount)
	}

	// Keep trying this call until it works. This is the most important one.
	err = r.Job.Finish()
	if err != nil {
		return fmt.Errorf("Failed to finish job: %s", err)
	}

	logger.Info("Finished job %s", r.Job.ID)

	return nil
}
