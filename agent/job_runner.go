package agent

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/bootstrap/shell"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/process"
	"github.com/buildkite/agent/retry"
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

	// The internal header time streamer
	headerTimesStreamer *HeaderTimesStreamer

	// The internal log streamer
	logStreamer *LogStreamer

	// If the job is being cancelled
	cancelled bool

	// Used to wait on various routines that we spin up
	routineWaitGroup sync.WaitGroup

	// A lock to protect concurrent calls to kill
	killLock sync.Mutex

	// File containing a copy of the job env
	envFile *os.File
}

// Initializes the job runner
func (r JobRunner) Create() (runner *JobRunner, err error) {
	runner = &r

	// Our own APIClient using the endpoint and the agents access token
	runner.APIClient = APIClient{Endpoint: r.Endpoint, Token: r.Agent.AccessToken}.Create()

	// // Create our header times struct
	runner.headerTimesStreamer = &HeaderTimesStreamer{UploadCallback: r.onUploadHeaderTime}

	// The log streamer that will take the output chunks, and send them to
	// the Buildkite Agent API
	runner.logStreamer = LogStreamer{MaxChunkSizeBytes: r.Job.ChunksMaxSizeBytes, Callback: r.onUploadChunk}.New()

	// Prepare a file to recieve the given job environment
	if file, err := ioutil.TempFile("", fmt.Sprintf("job-env-%s", runner.Job.ID)); err != nil {
		return runner, err
	} else {
		logger.Debug("[JobRunner] Created env file: %s", file.Name())
		runner.envFile = file
	}

	env, err := r.createEnvironment()
	if err != nil {
		return nil, err
	}

	args, err := shell.Parse(r.AgentConfiguration.BootstrapScript)
	if err != nil {
		return nil, err
	}

	logger.Debug("[JobRunner] Parsed bootstrap-script %q into %v",
		r.AgentConfiguration.BootstrapScript,
		args)

	// The process that will run the bootstrap script
	runner.process = &process.Process{
		Script:             args,
		Env:                env,
		PTY:                r.AgentConfiguration.RunInPty,
		Timestamp:          r.AgentConfiguration.TimestampLines,
		StartCallback:      r.onProcessStartCallback,
		LineCallback:       runner.headerTimesStreamer.Scan,
		LinePreProcessor:   runner.headerTimesStreamer.LinePreProcessor,
		LineCallbackFilter: runner.headerTimesStreamer.LineIsHeader,
	}

	return
}

// Runs the job
func (r *JobRunner) Run() error {
	logger.Info("Starting job %s", r.Job.ID)

	// Start the build in the Buildkite Agent API. This is the first thing
	// we do so if it fails, we don't have to worry about cleaning things
	// up like started log streamer workers, etc.
	if err := r.startJob(time.Now()); err != nil {
		return err
	}

	// Start the header time streamer
	if err := r.headerTimesStreamer.Start(); err != nil {
		return err
	}

	// Start the log streamer
	if err := r.logStreamer.Start(); err != nil {
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

	// Store the finished at time
	finishedAt := time.Now()

	// Stop the header time streamer. This will block until all the chunks
	// have been uploaded
	r.headerTimesStreamer.Stop()

	// Stop the log streamer. This will block until all the chunks have
	// been uploaded
	r.logStreamer.Stop()

	// Warn about failed chunks
	if r.logStreamer.ChunksFailedCount > 0 {
		logger.Warn("%d chunks failed to upload for this job", r.logStreamer.ChunksFailedCount)
	}

	// Finish the build in the Buildkite Agent API
	r.finishJob(finishedAt, r.process.ExitStatus, int(r.logStreamer.ChunksFailedCount))

	// Wait for the routines that we spun up to finish
	logger.Debug("[JobRunner] Waiting for all other routines to finish")
	r.routineWaitGroup.Wait()

	// Remove the env file, if any
	if r.envFile != nil {
		if err := os.Remove(r.envFile.Name()); err != nil {
			logger.Warn("[JobRunner] Error cleaning up env file: %s", err)
		}
		logger.Debug("[JobRunner] Deleted env file: %s", r.envFile.Name())
	}

	logger.Info("Finished job %s", r.Job.ID)

	return nil
}

func (r *JobRunner) Kill() error {
	r.killLock.Lock()
	defer r.killLock.Unlock()

	if !r.cancelled {
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

// Creates the environment variables that will be used in the process and writes a flat environment file
func (r *JobRunner) createEnvironment() ([]string, error) {
	// Create a clone of our jobs environment. We'll then set the
	// environment variables provided by the agent, which will override any
	// sent by Buildkite. The variables below should always take
	// precedence.
	env := make(map[string]string)
	for key, value := range r.Job.Env {
		env[key] = value
	}

	// Write out the job environment to a file, in k="v" format, with newlines escaped
	// We present only the clean environment - i.e only variables configured
	// on the job upstream - and expose the path in another environment variable.
	if r.envFile != nil {
		for key, value := range env {
			if _, err := r.envFile.WriteString(fmt.Sprintf("%s=%q\n", key, value)); err != nil {
				return nil, err
			}
		}
		if err := r.envFile.Close(); err != nil {
			return nil, err
		}
		env["BUILDKITE_ENV_FILE"] = r.envFile.Name()
	}

	// Add agent environment variables
	env["BUILDKITE_AGENT_ENDPOINT"] = r.Endpoint
	env["BUILDKITE_AGENT_ACCESS_TOKEN"] = r.Agent.AccessToken
	env["BUILDKITE_AGENT_DEBUG"] = fmt.Sprintf("%t", logger.GetLevel() == logger.DEBUG)
	env["BUILDKITE_AGENT_PID"] = fmt.Sprintf("%d", os.Getpid())

	// We know the BUILDKITE_BIN_PATH dir, because it's the path to the
	// currently running file (there is only 1 binary)
	dir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	env["BUILDKITE_BIN_PATH"] = dir

	// Add options from the agent configuration
	env["BUILDKITE_BUILD_PATH"] = r.AgentConfiguration.BuildPath
	env["BUILDKITE_HOOKS_PATH"] = r.AgentConfiguration.HooksPath
	env["BUILDKITE_PLUGINS_PATH"] = r.AgentConfiguration.PluginsPath
	env["BUILDKITE_SSH_KEYSCAN"] = fmt.Sprintf("%t", r.AgentConfiguration.SSHKeyscan)
	env["BUILDKITE_COMMAND_EVAL"] = fmt.Sprintf("%t", r.AgentConfiguration.CommandEval)
	env["BUILDKITE_PLUGINS_ENABLED"] = fmt.Sprintf("%t", r.AgentConfiguration.PluginsEnabled)
	env["BUILDKITE_GIT_CLONE_FLAGS"] = r.AgentConfiguration.GitCloneFlags
	env["BUILDKITE_GIT_CLEAN_FLAGS"] = r.AgentConfiguration.GitCleanFlags
	env["BUILDKITE_SHELL"] = r.AgentConfiguration.Shell

	// Convert the env map into a slice (which is what the script gear
	// needs)
	envSlice := []string{}
	for key, value := range env {
		envSlice = append(envSlice, fmt.Sprintf("%s=%s", key, value))
	}

	return envSlice, nil
}

// Starts the job in the Buildkite Agent API. We'll retry on connection-related
// issues, but if a connection succeeds and we get an error response back from
// Buildkite, we won't bother retrying. For example, a "no such host" will
// retry, but a 422 from Buildkite won't.
func (r *JobRunner) startJob(startedAt time.Time) error {
	r.Job.StartedAt = startedAt.UTC().Format(time.RFC3339Nano)

	return retry.Do(func(s *retry.Stats) error {
		_, err := r.APIClient.Jobs.Start(r.Job)

		if err != nil {
			if api.IsRetryableError(err) {
				logger.Warn("%s (%s)", err, s)
			} else {
				logger.Warn("Buildkite rejected the call to start the job (%s)", err)
				s.Break()
			}
		}

		return err
	}, &retry.Config{Maximum: 30, Interval: 5 * time.Second})
}

// Finishes the job in the Buildkite Agent API. This call will keep on retrying
// forever until it finally gets a successfull response from the API.
func (r *JobRunner) finishJob(finishedAt time.Time, exitStatus string, failedChunkCount int) error {
	r.Job.FinishedAt = finishedAt.UTC().Format(time.RFC3339Nano)
	r.Job.ExitStatus = exitStatus
	r.Job.ChunksFailedCount = failedChunkCount

	return retry.Do(func(s *retry.Stats) error {
		response, err := r.APIClient.Jobs.Finish(r.Job)
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
				logger.Warn("Buildkite rejected the call to finish the job (%s)", err)
				s.Break()
			} else {
				logger.Warn("%s (%s)", err, s)
			}
		}

		return err
	}, &retry.Config{Forever: true, Interval: 1 * time.Second})
}

func (r *JobRunner) onProcessStartCallback() {
	// Since we're spinning up 2 routines here, we might as well add them
	// to the routine wait group here.
	r.routineWaitGroup.Add(2)

	// Start a routine that will grab the output every few seconds and send
	// it back to Buildkite
	go func() {
		for r.process.IsRunning() {
			// Send the output of the process to the log streamer
			// for processing
			r.logStreamer.Process(r.process.Output())

			// Check the output in another second
			time.Sleep(1 * time.Second)
		}

		// Mark this routine as done in the wait group
		r.routineWaitGroup.Done()

		logger.Debug("[JobRunner] Routine that processes the log has finished")
	}()

	// Start a routine that will constantly ping Buildkite to see if the
	// job has been canceled
	go func() {
		for r.process.IsRunning() {
			// Re-get the job and check it's status to see if it's been
			// cancelled
			jobState, _, err := r.APIClient.Jobs.GetState(r.Job.ID)
			if err != nil {
				// We don't really care if it fails, we'll just
				// try again soon anyway
				logger.Warn("Problem with getting job state %s (%s)", r.Job.ID, err)
			} else if jobState.State == "canceling" || jobState.State == "canceled" {
				r.Kill()
			}

			// Check for cancellations
			time.Sleep(time.Duration(r.Agent.JobStatusInterval) * time.Second)
		}

		// Mark this routine as done in the wait group
		r.routineWaitGroup.Done()

		logger.Debug("[JobRunner] Routine that refreshes the job has finished")
	}()
}

func (r *JobRunner) onUploadHeaderTime(cursor int, total int, times map[string]string) {
	retry.Do(func(s *retry.Stats) error {
		_, err := r.APIClient.HeaderTimes.Save(r.Job.ID, &api.HeaderTimes{Times: times})
		if err != nil {
			logger.Warn("%s (%s)", err, s)
		}

		return err
	}, &retry.Config{Maximum: 10, Interval: 5 * time.Second})
}

// Call when a chunk is ready for upload. It retry the chunk upload with an
// interval before giving up.
func (r *JobRunner) onUploadChunk(chunk *LogStreamerChunk) error {
	return retry.Do(func(s *retry.Stats) error {
		_, err := r.APIClient.Chunks.Upload(r.Job.ID, &api.Chunk{
			Data:     chunk.Data,
			Sequence: chunk.Order,
			Offset:   chunk.Offset,
			Size:     chunk.Size,
		})
		if err != nil {
			logger.Warn("%s (%s)", err, s)
		}

		return err
	}, &retry.Config{Maximum: 10, Interval: 5 * time.Second})
}
