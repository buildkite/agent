package buildkite

import (
	"fmt"
	"github.com/buildkite/agent/buildkite/logger"
	"github.com/buildkite/agent/buildkite/logstreamer"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// The Job struct uses strings for StartedAt and FinishedAt because
// if they were actual date objects, then when this struct is
// initialized they would have a default value of: 00:00:00.000000000.
// This causes problems for the Buildkite Agent API because it looks for
// the presence of values in these properties to determine if the build
// has finished.
type Job struct {
	ID string

	State string

	Env map[string]string

	HeaderTimes []string `json:"header_times,omitempty"`

	ExitStatus string `json:"exit_status,omitempty"`

	StartedAt string `json:"started_at,omitempty"`

	FinishedAt string `json:"finished_at,omitempty"`

	// If the job is currently being cancelled
	cancelled bool

	// The currently running process of the job
	process *Process
}

func (b Job) String() string {
	return fmt.Sprintf("Job{ID: %s, State: %s, StartedAt: %s, FinishedAt: %s, Process: %s}", b.ID, b.State, b.StartedAt, b.FinishedAt, b.process)
}

func (c *Client) JobFind(id string) (*Job, error) {
	// Create a new instance of a job that will be populated
	// by the client.
	var job Job

	// Find the job
	return &job, c.Get(&job, "jobs/"+id)
}

func (c *Client) JobAccept(job *Job) (*Job, error) {
	// Accept the job
	return job, c.Put(job, "jobs/"+job.ID+"/accept", job)
}

func (c *Client) JobUpdate(job *Job) (*Job, error) {
	// Create a new instance of a job that will be populated
	// with the updated data by the client
	var updatedJob Job

	// Return the job.
	return &updatedJob, c.Put(&updatedJob, "jobs/"+job.ID, job)
}

func (j *Job) Kill() error {
	if j.cancelled {
		// Already cancelled
	} else {
		logger.Info("Cancelling job %s", j.ID)
		j.cancelled = true

		if j.process != nil {
			j.process.Kill()
		} else {
			logger.Error("No process to kill")
		}
	}

	return nil
}

func (j *Job) Run(agent *Agent) error {
	logger.Info("Starting job %s", j.ID)

	// Create a clone of our jobs environment. We'll then set the
	// environment variables provided by the agent, which will override any
	// sent by Buildkite. The variables below should always take
	// precedence.
	env := make(map[string]string)
	for key, value := range j.Env {
		env[key] = value
	}

	// Add agent environment variables
	env["BUILDKITE_AGENT_ENDPOINT"] = agent.Client.URL
	env["BUILDKITE_AGENT_ACCESS_TOKEN"] = agent.Client.AuthorizationToken
	env["BUILDKITE_AGENT_VERSION"] = Version()
	env["BUILDKITE_AGENT_DEBUG"] = fmt.Sprintf("%t", logger.GetLevel() == logger.DEBUG)

	// We know the BUILDKITE_BIN_PATH dir, because it's the path to the
	// currently running file (there is only 1 binary)
	dir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	env["BUILDKITE_BIN_PATH"] = dir

	// Add misc options
	env["BUILDKITE_BUILD_PATH"] = normalizePath(agent.BuildPath)
	env["BUILDKITE_HOOKS_PATH"] = normalizePath(agent.HooksPath)
	env["BUILDKITE_AUTO_SSH_FINGERPRINT_VERIFICATION"] = fmt.Sprintf("%t", agent.AutoSSHFingerprintVerification)
	env["BUILDKITE_COMMAND_EVAL"] = fmt.Sprintf("%t", agent.CommandEval)

	// Convert the env map into a slice (which is what the script gear
	// needs)
	envSlice := []string{}
	for key, value := range env {
		envSlice = append(envSlice, fmt.Sprintf("%s=%s", key, value))
	}

	// Create and start our log streamer
	logStreamer, _ := logstreamer.New()
	logStreamer.Start()

	// This callback is called when the process starts
	startCallback := func(process *Process) {
		// Start a routine that will grab the output every few seconds and send it back to Buildkite
		go func() {
			for process.Running {
				// Save the output to the job
				logStreamer.Process(process.Output())

				// Post the update to the API
				// updatedJob, err := agent.Client.JobUpdate(j)
				// if err != nil {
				// 	// We don't really care if the job couldn't update at this point.
				// 	// This is just a partial update. We'll just let the job run
				// 	// and hopefully the host will fix itself before we finish.
				// 	logger.Warn("Problem with updating job %s (%s)", j.ID, err)
				// } else if updatedJob.State == "canceled" {
				// 	j.Kill()
				// }

				// Sleep for 1 second
				time.Sleep(1000 * time.Millisecond)
			}

			logger.Debug("Routine that sends job updates has finished")
		}()
	}

	// The regular expression used to match headers
	headerRegexp, err := regexp.Compile("^(?:---|\\+\\+\\+|~~~)\\s(.+)?$")
	if err != nil {
		logger.Error("Failed to compile header regular expression (%T: %v)", err, err)
	}

	// This callback is called for every line that is output by the process
	lineCallback := func(process *Process, line string) {
		if headerRegexp.MatchString(line) {
			// logger.Debug("Found header \"%s\", capturing current time", line)
			j.HeaderTimes = append(j.HeaderTimes, time.Now().UTC().Format(time.RFC3339Nano))
		}
	}

	// Initialze our process to run
	process := InitProcess(agent.BootstrapScript, envSlice, agent.RunInPty, startCallback, lineCallback)

	// Store the process so we can cancel it later.
	j.process = process

	// Mark the build as started
	j.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_, err = agent.Client.JobUpdate(j)
	if err != nil {
		// We don't care if the HTTP request failed here. We hope that it
		// starts working during the actual build.
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
	j.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	j.ExitStatus = j.process.ExitStatus

	// Stop the log streamer
	logStreamer.Stop()

	// Keep trying this call until it works. This is the most important one.
	for {
		_, err = agent.Client.JobUpdate(j)
		if err != nil {
			logger.Error("Problem with updating final job information %s (%s)", j.ID, err)

			// How long should we wait until we try again?
			idleSeconds := 5

			// Sleep for a while
			sleepTime := time.Duration(idleSeconds*1000) * time.Millisecond
			time.Sleep(sleepTime)
		} else {
			break
		}
	}

	logger.Info("Finished job %s", j.ID)

	return nil
}

// Replaces ~/ with the users home directory. The builds path may be configured
// as ~/.buildkite/hooks, and if it's set in a configration file (not on the
// command line) the OS won't automatically expand it.
func normalizePath(path string) string {
	if len(path) > 2 && path[:2] == "~/" {
		usr, _ := user.Current()
		dir := usr.HomeDir
		return strings.Replace(path, "~", dir, 1)
	} else {
		return path
	}
}
