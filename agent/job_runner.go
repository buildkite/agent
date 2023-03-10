package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/experiments"
	"github.com/buildkite/agent/v3/hook"
	"github.com/buildkite/agent/v3/job/shell"
	"github.com/buildkite/agent/v3/kubernetes"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/metrics"
	"github.com/buildkite/agent/v3/process"
	"github.com/buildkite/agent/v3/status"
	"github.com/buildkite/roko"
	"github.com/buildkite/shellwords"
)

const (
	// BuildkiteMessageMax is the maximum length of "BUILDKITE_MESSAGE=...\0"
	// environment entry passed to job executor, beyond which it will be truncated
	// to avoid exceeding the system limit. Note that it includes the variable
	// name, equals sign, and null terminator.
	//
	// The true limit varies by system and may be shared with other env/argv
	// data. We'll settle on an arbitrary generous but reasonable value, and
	// adjust it if issues arise.
	//
	// macOS 10.15:    256 KiB shared by environment & argv
	// Linux 4.19:     128 KiB per k=v env
	// Windows 10:  16,384 KiB shared
	// POSIX:            4 KiB minimum shared
	BuildkiteMessageMax = 64 * 1024

	// BuildkiteMessageName is the env var name of the build/commit message.
	BuildkiteMessageName = "BUILDKITE_MESSAGE"
)

// Certain env can only be set by agent configuration.
// We show the user a warning in the bootstrap if they use any of these at a job level.
var ProtectedEnv = map[string]struct{}{
	"BUILDKITE_AGENT_ENDPOINT":           {},
	"BUILDKITE_AGENT_ACCESS_TOKEN":       {},
	"BUILDKITE_AGENT_DEBUG":              {},
	"BUILDKITE_AGENT_PID":                {},
	"BUILDKITE_BIN_PATH":                 {},
	"BUILDKITE_CONFIG_PATH":              {},
	"BUILDKITE_BUILD_PATH":               {},
	"BUILDKITE_GIT_MIRRORS_PATH":         {},
	"BUILDKITE_GIT_MIRRORS_SKIP_UPDATE":  {},
	"BUILDKITE_HOOKS_PATH":               {},
	"BUILDKITE_PLUGINS_PATH":             {},
	"BUILDKITE_SSH_KEYSCAN":              {},
	"BUILDKITE_GIT_SUBMODULES":           {},
	"BUILDKITE_COMMAND_EVAL":             {},
	"BUILDKITE_PLUGINS_ENABLED":          {},
	"BUILDKITE_LOCAL_HOOKS_ENABLED":      {},
	"BUILDKITE_GIT_CLONE_FLAGS":          {},
	"BUILDKITE_GIT_FETCH_FLAGS":          {},
	"BUILDKITE_GIT_CLONE_MIRROR_FLAGS":   {},
	"BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT": {},
	"BUILDKITE_GIT_CLEAN_FLAGS":          {},
	"BUILDKITE_SHELL":                    {},
}

type JobRunnerConfig struct {
	// The configuration of the agent from the CLI
	AgentConfiguration AgentConfiguration

	// What signal to use for worker cancellation
	CancelSignal process.Signal

	// Whether to set debug in the job
	Debug bool

	// Whether to set debug HTTP Requests in the job
	DebugHTTP bool

	// Stdout of the parent agent process. Used for job log stdout writing arg, for simpler containerized log collection.
	AgentStdout io.Writer
}

type jobRunner interface {
	Run(ctx context.Context) error
	CancelAndStop() error
}

type JobRunner struct {
	// The configuration for the job runner
	conf JobRunnerConfig

	// The logger to use
	logger logger.Logger

	// The registered agent API record running this job
	agent *api.AgentRegisterResponse

	// The job being run
	job *api.Job

	// The APIClient that will be used when updating the job
	apiClient APIClient

	// A scope for metrics within a job
	metrics *metrics.Scope

	// The internal process of the job
	process jobAPI

	// The internal buffer of the process output
	output *process.Buffer

	// The internal header time streamer
	headerTimesStreamer *headerTimesStreamer

	// The internal log streamer
	logStreamer *LogStreamer

	// If the job is being cancelled
	cancelled bool

	// If the agent is being stopped
	stopped bool

	// A lock to protect concurrent calls to cancel
	cancelLock sync.Mutex

	// File containing a copy of the job env
	envFile *os.File
}

type jobAPI interface {
	Done() <-chan struct{}
	Started() <-chan struct{}
	Interrupt() error
	Terminate() error
	Run(ctx context.Context) error
	WaitStatus() process.WaitStatus
}

var _ jobRunner = (*JobRunner)(nil)

// Initializes the job runner
func NewJobRunner(l logger.Logger, scope *metrics.Scope, ag *api.AgentRegisterResponse, job *api.Job, apiClient APIClient, conf JobRunnerConfig) (jobRunner, error) {
	runner := &JobRunner{
		agent:     ag,
		job:       job,
		logger:    l,
		conf:      conf,
		metrics:   scope,
		apiClient: apiClient,
	}

	// If the accept response has a token attached, we should use that instead of the Agent Access Token that
	// our current apiClient is using
	if job.Token != "" {
		clientConf := apiClient.Config()
		clientConf.Token = job.Token
		runner.apiClient = api.NewClient(l, clientConf)
	}

	// Create our header times struct
	runner.headerTimesStreamer = newHeaderTimesStreamer(l, runner.onUploadHeaderTime)

	// The log streamer that will take the output chunks, and send them to
	// the Buildkite Agent API
	runner.logStreamer = NewLogStreamer(l, runner.onUploadChunk, LogStreamerConfig{
		Concurrency:       3,
		MaxChunkSizeBytes: job.ChunksMaxSizeBytes,
	})

	// TempDir is not guaranteed to exist
	tempDir := os.TempDir()
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		// Actual file permissions will be reduced by umask, and won't be 0777 unless the user has manually changed the umask to 000
		if err = os.MkdirAll(tempDir, 0777); err != nil {
			return nil, err
		}
	}

	// Prepare a file to recieve the given job environment
	if file, err := os.CreateTemp(tempDir, fmt.Sprintf("job-env-%s", job.ID)); err != nil {
		return runner, err
	} else {
		l.Debug("[JobRunner] Created env file: %s", file.Name())
		runner.envFile = file
	}

	env, err := runner.createEnvironment()
	if err != nil {
		return nil, err
	}

	// The job executor script gets parsed based on the operating system
	cmd, err := shellwords.Split(conf.AgentConfiguration.JobRunScript)
	if err != nil {
		return nil, fmt.Errorf("Failed to split job executor script (%q) into tokens: %v",
			conf.AgentConfiguration.JobRunScript, err)
	}

	// Our log streamer works off a buffer of output
	runner.output = &process.Buffer{}

	pr, pw := io.Pipe()

	allWriters := []io.Writer{}

	switch {
	case experiments.IsEnabled(experiments.ANSITimestamps):
		// If we have ansi-timestamps, we can skip line timestamps AND header times
		// this is the future of timestamping
		prefixer := process.NewPrefixer(runner.output, func() string {
			return fmt.Sprintf("\x1b_bk;t=%d\x07",
				time.Now().UnixNano()/int64(time.Millisecond))
		})
		allWriters = append(allWriters, prefixer)

	case conf.AgentConfiguration.TimestampLines:
		// If we have timestamp lines on, we have to buffer lines before we flush them
		// because we need to know if the line is a header or not. It's a bummer.
		allWriters = append(allWriters, pw)

		go func() {
			// Use a scanner to process output line by line
			err := process.NewScanner(l).ScanLines(pr, func(line string) {
				// Send to our header streamer and determine if it's a header
				isHeader := runner.headerTimesStreamer.Scan(line)

				// Prefix non-header log lines with timestamps
				if !(isHeaderExpansion(line) || isHeader) {
					line = fmt.Sprintf("[%s] %s", time.Now().UTC().Format(time.RFC3339), line)
				}

				// Write the log line to the buffer
				_, _ = runner.output.Write([]byte(line + "\n"))
			})
			if err != nil {
				l.Error("[JobRunner] Encountered error %v", err)
			}
		}()

	default:
		// Write output directly to the line buffer so we
		allWriters = append(allWriters, pw, runner.output)

		// Use a scanner to process output for headers only
		go func() {
			err := process.NewScanner(l).ScanLines(pr, func(line string) {
				runner.headerTimesStreamer.Scan(line)
			})
			if err != nil {
				l.Error("[JobRunner] Encountered error %v", err)
			}
		}()
	}

	// if agent config "EnableJobLogTmpfile" is set, we extend the processWriter to write to a temporary file.
	// BUILDKITE_JOB_LOG_TMPFILE is an environment variable that contains the path to this temporary file.
	var tmpFile *os.File
	if conf.AgentConfiguration.EnableJobLogTmpfile {
		tmpFile, err = os.CreateTemp("", "buildkite_job_log")
		if err != nil {
			return nil, err
		}
		os.Setenv("BUILDKITE_JOB_LOG_TMPFILE", tmpFile.Name())
		allWriters = append(allWriters, tmpFile)
	}

	if conf.AgentConfiguration.WriteJobLogsToStdout {
		if conf.AgentConfiguration.LogFormat == "json" {
			log := newJobLogger(
				conf.AgentStdout, logger.StringField("org", job.Env["BUILDKITE_ORGANIZATION_SLUG"]),
				logger.StringField("pipeline", job.Env["BUILDKITE_PIPELINE_SLUG"]),
				logger.StringField("branch", job.Env["BUILDKITE_BRANCH"]),
				logger.StringField("queue", job.Env["BUILDKITE_AGENT_META_DATA_QUEUE"]),
				logger.StringField("job_id", job.ID),
			)
			allWriters = append(allWriters, log)
		} else {
			allWriters = append(allWriters, conf.AgentStdout)
		}
	}

	// The writer that output from the process goes into
	processWriter := io.MultiWriter(allWriters...)

	// Copy the current processes ENV and merge in the new ones. We do this
	// so the sub process gets PATH and stuff. We merge our path in over
	// the top of the current one so the ENV from Buildkite and the agent
	// take precedence over the agent
	processEnv := append(os.Environ(), env...)

	// The process that will run the bootstrap script
	if experiments.IsEnabled(experiments.KubernetesExec) {
		containerCount, err := strconv.Atoi(os.Getenv("BUILDKITE_CONTAINER_COUNT"))
		if err != nil {
			return nil, fmt.Errorf("failed to parse BUILDKITE_CONTAINER_COUNT: %w", err)
		}
		runner.process = kubernetes.New(l, kubernetes.Config{
			AccessToken: apiClient.Config().Token,
			Stdout:      processWriter,
			Stderr:      processWriter,
			ClientCount: containerCount,
		})
	} else {
		// The process that will run the job executor script
		runner.process = process.New(l, process.Config{
			Path:            cmd[0],
			Args:            cmd[1:],
			Dir:             conf.AgentConfiguration.BuildPath,
			Env:             processEnv,
			PTY:             conf.AgentConfiguration.RunInPty,
			Stdout:          processWriter,
			Stderr:          processWriter,
			InterruptSignal: conf.CancelSignal,
		})
	}

	// Close the writer end of the pipe when the process finishes
	go func() {
		<-runner.process.Done()
		if err := pw.Close(); err != nil {
			l.Error("%v", err)
		}
		if tmpFile != nil {
			if err := os.Remove(tmpFile.Name()); err != nil {
				l.Error("%v", err)
			}
		}
	}()

	return runner, nil
}

type hookExit struct {
	ExitStatus   int
	SignalReason string
	Ok           bool
}

func (r *JobRunner) preExecHook(ctx context.Context, hookName string) hookExit {
	exit := hookExit{Ok: true}
	hookPath, _ := hook.Find(r.conf.AgentConfiguration.HooksPath, hookName)
	if hookPath == "" {
		return exit
	}

	// Once we have a hook any failure to run it MUST be fatal to the job to guarantee a true
	// positive result from the hook
	okay, err := r.executePreExecHook(ctx, hookName, hookPath)
	if !okay {
		exit.Ok = false

		// Ensure the Job UI knows why this job resulted in failure
		r.logStreamer.Process([]byte(fmt.Sprintf("%s hook rejected this job, see the buildkite-agent logs for more details", hookName)))
		// But disclose more information in the agent logs
		r.logger.Error("%s hook rejected this job: %s", hookName, err)

		exit.ExitStatus = -1
		exit.SignalReason = "agent_refused"
	}

	return exit
}

// Runs the job
func (r *JobRunner) Run(ctx context.Context) error {
	r.logger.Info("Starting job %s", r.job.ID)

	ctx, done := status.AddItem(ctx, "Job Runner", "", nil)
	defer done()

	startedAt := time.Now()

	// Start the build in the Buildkite Agent API. This is the first thing
	// we do so if it fails, we don't have to worry about cleaning things
	// up like started log streamer workers, and so on.
	if err := r.startJob(ctx, startedAt); err != nil {
		return err
	}

	// If this agent successfully grabs the job from the API, publish metric for
	// how long this job was in the queue for, if we can calculate that
	if r.job.RunnableAt != "" {
		runnableAt, err := time.Parse(time.RFC3339Nano, r.job.RunnableAt)
		if err != nil {
			r.logger.Error("Metric submission failed to parse %s", r.job.RunnableAt)
		} else {
			r.metrics.Timing("queue.duration", startedAt.Sub(runnableAt))
		}
	}

	// Start the header time streamer
	go r.headerTimesStreamer.Run(ctx)

	// Start the log streamer. Launches multiple goroutines.
	if err := r.logStreamer.Start(ctx); err != nil {
		return err
	}

	hookExit := r.preExecHooks(ctx)
	// Before executing the job process with the received Job env,
	// execute the pre-bootstrap/pre-exec hook (if present) for it to tell us
	// whether it is happy to proceed.
	// Default exit status is no exit status
	signal := ""
	exitStatus := strconv.Itoa(hookExit.ExitStatus)
	signalReason := hookExit.SignalReason

	var wg sync.WaitGroup
	// Set up a child context for helper goroutines related to running the job.
	cctx, cancel := context.WithCancel(ctx)
	defer func() {
		// Finish the build in the Buildkite Agent API
		//
		// Once we tell the API we're finished it might assign us new work, so make
		// sure everything else is done first.
		r.finishJob(ctx, startedAt, cancel, &wg, exitStatus, signal, signalReason)

		r.logger.Info("Finished job %s", r.job.ID)
	}()

	if !hookExit.Ok {
		return nil
	}

	// Kick off log streaming and job status checking when the process starts.
	wg.Add(2)
	go r.jobLogStreamer(cctx, &wg)
	go r.jobCancellationChecker(cctx, &wg)

	// Run the process. This will block until it finishes.
	if err := r.process.Run(cctx); err != nil {
		// Send the error as output
		r.logStreamer.Process([]byte(err.Error()))

		// The process did not run at all, so make sure it fails
		exitStatus = "-1"
		signalReason = "process_run_error"
	} else {
		// Intended to capture situations where the job-exec (aka bootstrap) container did not
		// start. Normally such errors are hidden in the Kubernetes events. Let's feed them up
		// to the user as they may be the caused by errors in the pipeline definition.
		if r.cancelled && !r.stopped {
			k8sProcess, ok := r.process.(*kubernetes.Runner)
			if ok && k8sProcess.ClientStateUnknown() {
				r.logStreamer.Process([]byte(
					"Some containers had unknown exit statuses. Perhaps they were in ImagePullBackOff.",
				))
			}
		}

		// Add the final output to the streamer
		r.logStreamer.Process(r.output.ReadAndTruncate())

		// Collect the finished process' exit status
		exitStatus = fmt.Sprintf("%d", r.process.WaitStatus().ExitStatus())
		if ws := r.process.WaitStatus(); ws.Signaled() {
			signal = process.SignalString(ws.Signal())
		}
		if r.stopped {
			// The agent is being gracefully stopped, and we signaled the job to end. Often due
			// to pending host shutdown or EC2 spot instance termination
			signalReason = "agent_stop"
		} else if r.cancelled {
			// The job was signaled because it was cancelled via the buildkite web UI
			signalReason = "cancel"
		}
	}

	return nil
}

func (r *JobRunner) preExecHooks(ctx context.Context) hookExit {
	hookExit := r.preExecHook(ctx, "pre-bootstrap")
	if !hookExit.Ok {
		return hookExit
	}

	return r.preExecHook(ctx, "pre-exec")

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
		reason = " (agent stopping)"
	}
	r.logger.Info("Canceling job %s with a grace period of %ds%s",
		r.job.ID, r.conf.AgentConfiguration.CancelGracePeriod, reason)

	r.cancelled = true

	// First we interrupt the process (ctrl-c or SIGINT)
	if err := r.process.Interrupt(); err != nil {
		return err
	}

	select {
	// Grace period for cancelling
	case <-time.After(time.Second * time.Duration(r.conf.AgentConfiguration.CancelGracePeriod)):
		r.logger.Info("Job %s hasn't stopped in time, terminating", r.job.ID)

		// Terminate the process as we've exceeded our context
		return r.process.Terminate()

	// Process successfully terminated
	case <-r.process.Done():
		return nil
	}
}

// Creates the environment variables that will be used in the process and writes a flat environment file
func (r *JobRunner) createEnvironment() ([]string, error) {
	// Create a clone of our jobs environment. We'll then set the
	// environment variables provided by the agent, which will override any
	// sent by Buildkite. The variables below should always take
	// precedence.
	env := make(map[string]string)
	for key, value := range r.job.Env {
		env[key] = value
	}

	// The agent registration token should never make it into the job environment
	delete(env, "BUILDKITE_AGENT_TOKEN")

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

	var ignoredEnv []string

	// Check if the user has defined any protected env
	for k := range ProtectedEnv {
		if _, exists := r.job.Env[k]; exists {
			ignoredEnv = append(ignoredEnv, k)
		}
	}

	// Set BUILDKITE_IGNORED_ENV so the job executor can show warnings
	if len(ignoredEnv) > 0 {
		env["BUILDKITE_IGNORED_ENV"] = strings.Join(ignoredEnv, ",")
	}

	// Add the API configuration
	apiConfig := r.apiClient.Config()
	env["BUILDKITE_AGENT_ENDPOINT"] = apiConfig.Endpoint
	env["BUILDKITE_AGENT_ACCESS_TOKEN"] = apiConfig.Token

	// Add agent environment variables
	env["BUILDKITE_AGENT_DEBUG"] = fmt.Sprintf("%t", r.conf.Debug)
	env["BUILDKITE_AGENT_DEBUG_HTTP"] = fmt.Sprintf("%t", r.conf.DebugHTTP)
	env["BUILDKITE_AGENT_PID"] = fmt.Sprintf("%d", os.Getpid())

	// We know the BUILDKITE_BIN_PATH dir, because it's the path to the
	// currently running file (there is only 1 binary)
	exePath, err := os.Executable()
	if err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(filepath.Dir(exePath))
	if err != nil {
		return nil, err
	}
	env["BUILDKITE_BIN_PATH"] = dir

	// Add options from the agent configuration
	env["BUILDKITE_CONFIG_PATH"] = r.conf.AgentConfiguration.ConfigPath
	env["BUILDKITE_BUILD_PATH"] = r.conf.AgentConfiguration.BuildPath
	env["BUILDKITE_SOCKETS_PATH"] = r.conf.AgentConfiguration.SocketsPath
	env["BUILDKITE_GIT_MIRRORS_PATH"] = r.conf.AgentConfiguration.GitMirrorsPath
	env["BUILDKITE_GIT_MIRRORS_SKIP_UPDATE"] = fmt.Sprintf("%t", r.conf.AgentConfiguration.GitMirrorsSkipUpdate)
	env["BUILDKITE_HOOKS_PATH"] = r.conf.AgentConfiguration.HooksPath
	env["BUILDKITE_PLUGINS_PATH"] = r.conf.AgentConfiguration.PluginsPath
	env["BUILDKITE_SSH_KEYSCAN"] = fmt.Sprintf("%t", r.conf.AgentConfiguration.SSHKeyscan)
	env["BUILDKITE_GIT_SUBMODULES"] = fmt.Sprintf("%t", r.conf.AgentConfiguration.GitSubmodules)
	env["BUILDKITE_COMMAND_EVAL"] = fmt.Sprintf("%t", r.conf.AgentConfiguration.CommandEval)
	env["BUILDKITE_PLUGINS_ENABLED"] = fmt.Sprintf("%t", r.conf.AgentConfiguration.PluginsEnabled)
	env["BUILDKITE_LOCAL_HOOKS_ENABLED"] = fmt.Sprintf("%t", r.conf.AgentConfiguration.LocalHooksEnabled)
	env["BUILDKITE_GIT_CHECKOUT_FLAGS"] = r.conf.AgentConfiguration.GitCheckoutFlags
	env["BUILDKITE_GIT_CLONE_FLAGS"] = r.conf.AgentConfiguration.GitCloneFlags
	env["BUILDKITE_GIT_FETCH_FLAGS"] = r.conf.AgentConfiguration.GitFetchFlags
	env["BUILDKITE_GIT_CLONE_MIRROR_FLAGS"] = r.conf.AgentConfiguration.GitCloneMirrorFlags
	env["BUILDKITE_GIT_CLEAN_FLAGS"] = r.conf.AgentConfiguration.GitCleanFlags
	env["BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT"] = fmt.Sprintf("%d", r.conf.AgentConfiguration.GitMirrorsLockTimeout)
	env["BUILDKITE_SHELL"] = r.conf.AgentConfiguration.Shell
	env["BUILDKITE_AGENT_EXPERIMENT"] = strings.Join(experiments.Enabled(), ",")
	env["BUILDKITE_REDACTED_VARS"] = strings.Join(r.conf.AgentConfiguration.RedactedVars, ",")

	// propagate CancelSignal to job executor, unless it's the default SIGTERM
	if r.conf.CancelSignal != process.SIGTERM {
		env["BUILDKITE_CANCEL_SIGNAL"] = r.conf.CancelSignal.String()
	}

	// Whether to enable profiling in the job executor
	if r.conf.AgentConfiguration.Profile != "" {
		env["BUILDKITE_AGENT_PROFILE"] = r.conf.AgentConfiguration.Profile
	}

	// PTY-mode is enabled by default in `start` and `job-exec`, so we only need
	// to propagate it if it's explicitly disabled.
	if !r.conf.AgentConfiguration.RunInPty {
		env["BUILDKITE_PTY"] = "false"
	}

	enablePluginValidation := r.conf.AgentConfiguration.PluginValidation
	// Allow BUILDKITE_PLUGIN_VALIDATION to be enabled from env for easier
	// per-pipeline testing
	if pluginValidation, ok := env["BUILDKITE_PLUGIN_VALIDATION"]; ok {
		switch pluginValidation {
		case "true", "1", "on":
			enablePluginValidation = true
		}
	}
	env["BUILDKITE_PLUGIN_VALIDATION"] = fmt.Sprintf("%t", enablePluginValidation)

	if r.conf.AgentConfiguration.TracingBackend != "" {
		env["BUILDKITE_TRACING_BACKEND"] = r.conf.AgentConfiguration.TracingBackend
		env["BUILDKITE_TRACING_SERVICE_NAME"] = r.conf.AgentConfiguration.TracingServiceName
	}

	// see documentation for BuildkiteMessageMax
	if err := truncateEnv(r.logger, env, BuildkiteMessageName, BuildkiteMessageMax); err != nil {
		r.logger.Warn("failed to truncate %s: %v", BuildkiteMessageName, err)
		// attempt to continue anyway
	}

	// Convert the env map into a slice (which is what the script gear
	// needs)
	envSlice := []string{}
	for key, value := range env {
		envSlice = append(envSlice, fmt.Sprintf("%s=%s", key, value))
	}

	return envSlice, nil
}

// truncateEnv cuts environment variable `key` down to `max` length, such that
// "key=value\0" does not exceed the max.
func truncateEnv(l logger.Logger, env map[string]string, key string, max int) error {
	msglen := len(env[key])
	if msglen <= max {
		return nil
	}
	msgmax := max - len(key) - 2 // two bytes for "=" and null terminator
	description := fmt.Sprintf("value truncated %d -> %d bytes", msglen, msgmax)
	apology := fmt.Sprintf("[%s]", description)
	if len(apology) > msgmax {
		return fmt.Errorf("max=%d too short to include truncation apology", max)
	}
	keeplen := msgmax - len(apology)
	env[key] = env[key][0:keeplen] + apology
	l.Warn("%s %s", key, description)
	return nil
}

type LogWriter struct {
	l logger.Logger
}

func (w LogWriter) Write(bytes []byte) (int, error) {
	w.l.Info("%s", bytes)
	return len(bytes), nil
}

func (r *JobRunner) executePreExecHook(ctx context.Context, hookName, hookPath string) (bool, error) {
	r.logger.Info("Running %s hook %q", hookName, hookPath)

	sh, err := shell.New()
	if err != nil {
		return false, err
	}

	// This (plus inherited) is the only ENV that should be exposed
	// to the pre-exec hook.
	sh.Env.Set("BUILDKITE_ENV_FILE", r.envFile.Name())

	sh.Writer = LogWriter{
		l: r.logger,
	}

	if err := sh.RunWithoutPrompt(ctx, hookPath); err != nil {
		r.logger.Error("Finished %s hook %q: job rejected", hookName, hookPath)
		return false, err
	}

	r.logger.Info("Finished %s hook %q: job accepted", hookName, hookPath)
	return true, nil
}

// Starts the job in the Buildkite Agent API. We'll retry on connection-related
// issues, but if a connection succeeds and we get an client error response back from
// Buildkite, we won't bother retrying. For example, a "no such host" will
// retry, but an HTTP response from Buildkite that isn't retryable won't.
func (r *JobRunner) startJob(ctx context.Context, startedAt time.Time) error {
	r.job.StartedAt = startedAt.UTC().Format(time.RFC3339Nano)

	return roko.NewRetrier(
		roko.WithMaxAttempts(7),
		roko.WithStrategy(roko.Exponential(2*time.Second, 0)),
	).DoWithContext(ctx, func(rtr *roko.Retrier) error {
		response, err := r.apiClient.StartJob(ctx, r.job)

		if err != nil {
			if response != nil && api.IsRetryableStatus(response) {
				r.logger.Warn("%s (%s)", err, rtr)
			} else if api.IsRetryableError(err) {
				r.logger.Warn("%s (%s)", err, rtr)
			} else {
				r.logger.Warn("Buildkite rejected the call to start the job (%s)", err)
				rtr.Break()
			}
		}

		return err
	})
}

// finishJob finishes the job in the Buildkite Agent API. If the FinishJob call
// cannot return successfully, this will retry for a long time.
func (r *JobRunner) finishJob(ctx context.Context, startedAt time.Time, cancel func(), wg *sync.WaitGroup, exitStatus, signal, signalReason string) error {
	finishedAt := time.Now()

	// Stop the header time streamer. This will block until all the chunks
	// have been uploaded
	r.headerTimesStreamer.Stop()

	// Stop the log streamer. This will block until all the chunks have
	// been uploaded
	r.logStreamer.Stop()

	// Warn about failed chunks
	if count := r.logStreamer.FailedChunks(); count > 0 {
		r.logger.Warn("%d chunks failed to upload for this job", count)
	}

	// Ensure the additional goroutines are stopped.
	cancel()

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
	jobMetrics := r.metrics.With(metrics.Tags{
		"exit_code": exitStatus,
	})

	if exitStatus == "0" {
		jobMetrics.Timing("jobs.duration.success", finishedAt.Sub(startedAt))
		jobMetrics.Count("jobs.success", 1)
	} else {
		jobMetrics.Timing("jobs.duration.error", finishedAt.Sub(startedAt))
		jobMetrics.Count("jobs.failed", 1)
	}

	r.job.FinishedAt = finishedAt.UTC().Format(time.RFC3339Nano)
	r.job.ExitStatus = exitStatus
	r.job.Signal = signal
	r.job.SignalReason = signalReason
	r.job.ChunksFailedCount = r.logStreamer.FailedChunks()

	r.logger.Debug("[JobRunner] Finishing job with exit_status=%s, signal=%s and signal_reason=%s",
		r.job.ExitStatus, r.job.Signal, r.job.SignalReason)

	ctx, ccancel := context.WithTimeout(ctx, 48*time.Hour)
	defer ccancel()

	return roko.NewRetrier(
		roko.TryForever(),
		roko.WithJitter(),
		roko.WithStrategy(roko.Constant(1*time.Second)),
	).DoWithContext(ctx, func(retrier *roko.Retrier) error {
		response, err := r.apiClient.FinishJob(ctx, r.job)
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
		r.logStreamer.Process(r.output.ReadAndTruncate())

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

// jobCancellationChecker waits for the processs to start, then continuously
// polls GetJobState to see if the job has been cancelled server-side. If so,
// it calls r.Cancel.
func (r *JobRunner) jobCancellationChecker(ctx context.Context, wg *sync.WaitGroup) {
	ctx, setStat, done := status.AddSimpleItem(ctx, "Job Cancellation Checker")
	defer done()
	setStat("Starting...")

	defer func() {
		// Mark this routine as done in the wait group
		wg.Done()

		r.logger.Debug("[JobRunner] Routine that refreshes the job has finished")
	}()

	select {
	case <-r.process.Started():
	case <-ctx.Done():
		return
	}

	for {
		setStat("📡 Fetching job state from Buildkite")

		// Re-get the job and check its status to see if it's been cancelled
		jobState, _, err := r.apiClient.GetJobState(ctx, r.job.ID)
		if err != nil {
			// We don't really care if it fails, we'll just
			// try again soon anyway
			r.logger.Warn("Problem with getting job state %s (%s)", r.job.ID, err)
		} else if jobState.State == "canceling" || jobState.State == "canceled" {
			if err := r.Cancel(); err != nil {
				r.logger.Error("Unexpected error canceling process as requested by server (job: %s) (err: %s)", r.job.ID, err)
			}
		}

		setStat("😴 Sleeping for a bit")

		// Sleep for a bit, or until the job is finished
		select {
		case <-time.After(time.Duration(r.agent.JobStatusInterval) * time.Second):
		case <-ctx.Done():
			return
		case <-r.process.Done():
			return
		}
	}
}

func (r *JobRunner) onUploadHeaderTime(ctx context.Context, cursor, total int, times map[string]string) {
	roko.NewRetrier(
		roko.WithMaxAttempts(10),
		roko.WithStrategy(roko.Constant(5*time.Second)),
	).DoWithContext(ctx, func(retrier *roko.Retrier) error {
		response, err := r.apiClient.SaveHeaderTimes(ctx, r.job.ID, &api.HeaderTimes{Times: times})
		if err != nil {
			if response != nil && (response.StatusCode >= 400 && response.StatusCode <= 499) {
				r.logger.Warn("Buildkite rejected the header times (%s)", err)
				retrier.Break()
			} else {
				r.logger.Warn("%s (%s)", err, retrier)
			}
		}

		return err
	})
}

// onUploadChunk uploads a log streamer chunk. If a valid chunk cannot be
// uploaded, it will retry for a long time.
func (r *JobRunner) onUploadChunk(ctx context.Context, chunk *LogStreamerChunk) error {
	// We consider logs to be an important thing, and we shouldn't give up
	// on sending the chunk data back to Buildkite. In the event Buildkite
	// is having downtime or there are connection problems, we'll want to
	// hold onto chunks until it's back online to upload them.
	//
	// This code will retry for a long time until we get back a successful
	// response from Buildkite that it's considered the chunk (a 4xx will be
	// returned if the chunk is invalid, and we shouldn't retry on that)
	ctx, cancel := context.WithTimeout(ctx, 48*time.Hour)
	defer cancel()

	return roko.NewRetrier(
		roko.TryForever(),
		roko.WithStrategy(roko.Constant(5*time.Second)),
		roko.WithJitter(),
	).DoWithContext(ctx, func(retrier *roko.Retrier) error {
		response, err := r.apiClient.UploadChunk(ctx, r.job.ID, &api.Chunk{
			Data:     chunk.Data,
			Sequence: chunk.Order,
			Offset:   chunk.Offset,
			Size:     chunk.Size,
		})
		if err != nil {
			if response != nil && (response.StatusCode >= 400 && response.StatusCode <= 499) {
				r.logger.Warn("Buildkite rejected the chunk upload (%s)", err)
				retrier.Break()
			} else {
				r.logger.Warn("%s (%s)", err, retrier)
			}
		}

		return err
	})
}

// jobLogger is just a simple wrapper around a JSON Logger that satisfies the
// io.Writer interface so it can be seemlessly use with existing job logging code.
type jobLogger struct {
	log logger.Logger
}

func newJobLogger(stdout io.Writer, fields ...logger.Field) jobLogger {
	l := logger.NewConsoleLogger(logger.NewJSONPrinter(stdout), os.Exit)
	l = l.WithFields(logger.StringField("source", "job"))
	l = l.WithFields(fields...)
	return jobLogger{log: l}
}

// Write adapts the underlying JSON logger to match the io.Writer interface to
// easier slotting into job logger code. This will write existing fields
// attached to the logger, the message, and write out to the INFO level.
func (l jobLogger) Write(data []byte) (int, error) {
	// When writing as a structured log, trailing newlines and carriage returns
	// generally don't make sense.
	msg := strings.TrimRight(string(data), "\r\n")
	l.log.Info(msg)
	return len(data), nil
}
