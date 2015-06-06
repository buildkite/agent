package buildkite

import (
	"fmt"
	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/process"
	"github.com/buildkite/agent/retry"
	"os"
	"path/filepath"
	_ "regexp"
	"time"
)

type JobRunner struct {
	// The job being run
	Job *api.Job

	// The APIClient that will be used when updating the job
	APIClient *api.Client

	// The endpoint that should be used when communicating with the API
	Endpoint string

	// The registred agent API record running this job
	Agent *api.Agent

	// The configuration of the agent from the CLI
	AgentConfiguration *AgentConfiguration

	// The interal process of the job
	process *process.Process

	// The internal log streamer
	logStreamer *LogStreamer

	// If the job is being cancelled
	cancelled bool
}

// Initializes the job runner
func (r JobRunner) Create() (runner *JobRunner, err error) {
	runner = &r

	// Our own APIClient using the endpoint and the agents access token
	runner.APIClient = APIClient{Endpoint: r.Endpoint, Token: r.Agent.AccessToken}.Create()

	// // Create our header times struct
	// headerTimes := HeaderTimes{Job: r.Job, API: r.Job.API}

	// The log streamer that will take the output chunks, and send them to
	// the Buildkite Agent API
	runner.logStreamer = LogStreamer{MaxChunkSizeBytes: r.Job.ChunksMaxSizeBytes, Callback: r.onUploadChunk}.New()

	// The process that will run the bootstrap script
	runner.process = process.Process{
		Script:        r.AgentConfiguration.BootstrapScript,
		Env:           r.createEnvironment(),
		PTY:           r.AgentConfiguration.RunInPty,
		StartCallback: r.onProcessStartCallback,
		LineCallback:  r.onLineCallback,
	}.Create()

	return
}

// Runs the job
func (r *JobRunner) Run() error {
	logger.Info("Starting job %s", r.Job.ID)

	// Start the log streamer
	if err := r.logStreamer.Start(); err != nil {
		return err
	}

	// Start the build in the Buildkite Agent API
	if err := r.startJobInAPI(); err != nil {
		return err
	}

	// Start the process. This will block until it finishes.
	if err := r.process.Start(); err != nil {
		// Send the error as output
		r.logStreamer.Process(fmt.Sprintf("%s", err))
	} else {
		// Add the final output to the streamer
		r.logStreamer.Process(r.process.Output())
	}

	// // Wait until all the header times have finished uploading
	// headerTimes.Wait()

	// Stop the log streamer. This will block until all the chunks have
	// been uploaded
	r.logStreamer.Stop()

	// Warn about failed chunks
	if r.logStreamer.ChunksFailedCount > 0 {
		logger.Warn("%d chunks failed to upload for this job", r.logStreamer.ChunksFailedCount)
	}

	// Finish the build in the Buildkite Agent API
	r.finishJobInAPI(r.process.ExitStatus, int(r.logStreamer.ChunksFailedCount))

	logger.Info("Finished job %s", r.Job.ID)

	return nil
}

// Creates the environment variables that will be used in the process
func (r *JobRunner) createEnvironment() []string {
	// Create a clone of our jobs environment. We'll then set the
	// environment variables provided by the agent, which will override any
	// sent by Buildkite. The variables below should always take
	// precedence.
	env := make(map[string]string)
	for key, value := range r.Job.Env {
		env[key] = value
	}

	// Add agent environment variables
	env["BUILDKITE_AGENT_ENDPOINT"] = r.Endpoint
	env["BUILDKITE_AGENT_ACCESS_TOKEN"] = r.Agent.AccessToken
	env["BUILDKITE_AGENT_DEBUG"] = fmt.Sprintf("%t", logger.GetLevel() == logger.DEBUG)

	// We know the BUILDKITE_BIN_PATH dir, because it's the path to the
	// currently running file (there is only 1 binary)
	dir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	env["BUILDKITE_BIN_PATH"] = dir

	// Add misc options
	env["BUILDKITE_BUILD_PATH"] = r.AgentConfiguration.BuildPath
	env["BUILDKITE_HOOKS_PATH"] = r.AgentConfiguration.HooksPath
	env["BUILDKITE_AUTO_SSH_FINGERPRINT_VERIFICATION"] = fmt.Sprintf("%t", r.AgentConfiguration.AutoSSHFingerprintVerification)
	env["BUILDKITE_COMMAND_EVAL"] = fmt.Sprintf("%t", r.AgentConfiguration.CommandEval)

	// Convert the env map into a slice (which is what the script gear
	// needs)
	envSlice := []string{}
	for key, value := range env {
		envSlice = append(envSlice, fmt.Sprintf("%s=%s", key, value))
	}

	return envSlice
}

// Starts the job in the Buildkite Agent API
func (r *JobRunner) startJobInAPI() error {
	r.Job.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_, _, err := r.APIClient.Jobs.Start(r.Job)

	return err
}

// Finishes the job in the Buildkite Agent API. This call will keep on retrying
// forever until it finally gets a successfull response from the API.
func (r *JobRunner) finishJobInAPI(exitStatus string, failedChunkCount int) error {
	r.Job.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	r.Job.ExitStatus = exitStatus
	r.Job.ChunksFailedCount = failedChunkCount

	return retry.Do(func(s *retry.Stats) error {
		_, _, err := r.APIClient.Jobs.Finish(r.Job)
		if err != nil {
			logger.Warn("%s (%s)", err, s)
		}

		return err
	}, &retry.Config{Forever: true, Interval: 1 * time.Second})
}

func (r *JobRunner) onProcessStartCallback() {
	// Start a routine that will grab the output every few seconds and send
	// it back to Buildkite
	go func() {
		for r.process.Running {
			// Send the output of the process to the log streamer
			// for processing
			r.logStreamer.Process(r.process.Output())

			// Check the output in another second
			time.Sleep(1 * time.Second)
		}

		logger.Debug("Routine that processes the log has finished")
	}()

	// Start a routine that will grab the output every few seconds and send it back to Buildkite
	go func() {
		for r.process.Running {
			// Re-get the job and check it's status to see if it's been
			// cancelled
			job, _, err := r.APIClient.Jobs.Get(r.Job.ID)
			if err != nil {
				// We don't really care if it fails, we'll just
				// try again in a second anyway
				logger.Warn("Problem with getting job status %s (%s)", r.Job.ID, err)
			} else if job.State == "canceled" {
				r.Kill()
			}

			// Check for cancellations every few seconds
			time.Sleep(3 * time.Second)
		}

		logger.Debug("Routine that refreshes the job has finished")
	}()
}

func (r *JobRunner) onLineCallback(line string) {
	// // The regular expression used to match headers
	// headerRegexp, err := regexp.Compile("^(?:---|\\+\\+\\+|~~~)\\s(.+)?$")
	// if err != nil {
	// 	logger.Error("Failed to compile header regular expression (%T: %v)", err, err)
	// }
	// 	// We'll also ignore any line over 500 characters (who has a
	// 	// 500 character header...honestly...)
	// 	if len(line) < 500 && headerRegexp.MatchString(line) {
	// 		// logger.Debug("Found header \"%s\", capturing current time", line)
	// 		go headerTimes.Now(line)
	// 	}
}

// Call when a chunk is ready for upload
func (r *JobRunner) onUploadChunk(chunk *LogStreamerChunk) error {
	return retry.Do(func(s *retry.Stats) error {
		_, err := r.APIClient.Chunks.Upload(&api.Chunk{
			Job:      r.Job,
			Data:     chunk.Data,
			Sequence: chunk.Order,
		})

		if err != nil {
			logger.Warn("%s (%s)", err, s)
		}

		return err
	}, &retry.Config{Forever: true, Interval: 1 * time.Second})
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
