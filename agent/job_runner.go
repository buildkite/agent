package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/core"
	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/internal/job/shell"
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
	// environment entry passed to bootstrap, beyond which it will be truncated
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

	VerificationBehaviourWarn  = "warn"
	VerificationBehaviourBlock = "block"
)

// Certain env can only be set by agent configuration.
// We show the user a warning in the bootstrap if they use any of these at a job level.
var ProtectedEnv = map[string]struct{}{
	"BUILDKITE_AGENT_ACCESS_TOKEN":       {},
	"BUILDKITE_AGENT_DEBUG":              {},
	"BUILDKITE_AGENT_ENDPOINT":           {},
	"BUILDKITE_AGENT_PID":                {},
	"BUILDKITE_BIN_PATH":                 {},
	"BUILDKITE_BUILD_PATH":               {},
	"BUILDKITE_COMMAND_EVAL":             {},
	"BUILDKITE_CONFIG_PATH":              {},
	"BUILDKITE_CONTAINER_COUNT":          {},
	"BUILDKITE_GIT_CLEAN_FLAGS":          {},
	"BUILDKITE_GIT_CLONE_FLAGS":          {},
	"BUILDKITE_GIT_CLONE_MIRROR_FLAGS":   {},
	"BUILDKITE_GIT_FETCH_FLAGS":          {},
	"BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT": {},
	"BUILDKITE_GIT_MIRRORS_PATH":         {},
	"BUILDKITE_GIT_MIRRORS_SKIP_UPDATE":  {},
	"BUILDKITE_GIT_SUBMODULES":           {},
	"BUILDKITE_HOOKS_PATH":               {},
	"BUILDKITE_KUBERNETES_EXEC":          {},
	"BUILDKITE_LOCAL_HOOKS_ENABLED":      {},
	"BUILDKITE_PLUGINS_ENABLED":          {},
	"BUILDKITE_PLUGINS_PATH":             {},
	"BUILDKITE_SHELL":                    {},
	"BUILDKITE_SSH_KEYSCAN":              {},
}

type JobRunnerConfig struct {
	// The configuration of the agent from the CLI
	AgentConfiguration AgentConfiguration

	// How often to check if the job has been cancelled
	JobStatusInterval time.Duration

	// The JSON Web Keyset for verifying the job
	JWKS any

	// A scope for metrics within a job
	MetricsScope *metrics.Scope

	// The job to run
	Job *api.Job

	// What signal to use for worker cancellation
	CancelSignal process.Signal

	// Whether to set debug in the job
	Debug bool

	// Whether to set debug HTTP Requests in the job
	DebugHTTP bool

	// Whether the job is executing as a k8s pod
	KubernetesExec bool

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

	// How the JobRunner should respond when job verification fails (one of `block` or `warn`)
	VerificationFailureBehavior string

	// agentLogger is a agentLogger that outputs to the agent logs
	agentLogger logger.Logger

	// The APIClient that will be used when updating the job
	apiClient APIClient

	// The agentlib Client is used to drive some APIClient methods
	client *core.Client

	// The internal process of the job
	process jobAPI

	// The internal buffer of the process output
	output *process.Buffer

	// The internal header time streamer
	headerTimesStreamer *headerTimesStreamer

	// The internal log streamer. Don't write to this directly, use `jobLogs` instead
	logStreamer *LogStreamer

	// jobLogs is an io.Writer that sends data to the job logs
	jobLogs io.Writer

	// If the job is being cancelled
	cancelled bool

	// When the job was started
	startedAt time.Time

	// If the agent is being stopped
	stopped bool

	// A lock to protect concurrent calls to cancel
	cancelLock sync.Mutex

	// Files containing a copy of the job env
	envShellFile *os.File
	envJSONFile  *os.File
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
func NewJobRunner(ctx context.Context, l logger.Logger, apiClient APIClient, conf JobRunnerConfig) (jobRunner, error) {
	r := &JobRunner{
		agentLogger: l,
		conf:        conf,
		apiClient:   apiClient,
	}

	var err error
	r.VerificationFailureBehavior, err = r.normalizeVerificationBehavior(conf.AgentConfiguration.VerificationFailureBehaviour)
	if err != nil {
		return nil, fmt.Errorf("setting no signature behavior: %w", err)
	}

	if conf.JobStatusInterval == 0 {
		conf.JobStatusInterval = 1 * time.Second
	}

	// If the accept response has a token attached, we should use that instead of the Agent Access Token that
	// our current apiClient is using
	if r.conf.Job.Token != "" {
		clientConf := r.apiClient.Config()
		clientConf.Token = r.conf.Job.Token
		r.apiClient = api.NewClient(r.agentLogger, clientConf)
	}
	r.client = &core.Client{APIClient: r.apiClient, Logger: l}

	// Create our header times struct
	r.headerTimesStreamer = newHeaderTimesStreamer(r.agentLogger, r.onUploadHeaderTime)

	// The log streamer that will take the output chunks, and send them to
	// the Buildkite Agent API
	r.logStreamer = NewLogStreamer(
		r.agentLogger,
		func(ctx context.Context, chunk *api.Chunk) error {
			return r.client.UploadChunk(ctx, r.conf.Job.ID, chunk)
		},
		LogStreamerConfig{
			Concurrency:       3,
			MaxChunkSizeBytes: r.conf.Job.ChunksMaxSizeBytes,
			MaxSizeBytes:      r.conf.Job.LogMaxSizeBytes,
		},
	)

	// TempDir is not guaranteed to exist
	tempDir := os.TempDir()
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		// Actual file permissions will be reduced by umask, and won't be 0777 unless the user has manually changed the umask to 000
		if err = os.MkdirAll(tempDir, 0777); err != nil {
			return nil, err
		}
	}

	// Prepare a file to receive the given job environment
	file, err := os.CreateTemp(tempDir, fmt.Sprintf("job-env-%s", r.conf.Job.ID))
	if err != nil {
		return r, err
	}
	r.agentLogger.Debug("[JobRunner] Created env file (shell format): %s", file.Name())
	r.envShellFile = file

	file, err = os.CreateTemp(tempDir, fmt.Sprintf("job-env-json-%s", r.conf.Job.ID))
	if err != nil {
		return r, err
	}
	r.agentLogger.Debug("[JobRunner] Created env file (JSON format): %s", file.Name())
	r.envJSONFile = file

	env, err := r.createEnvironment(ctx)
	if err != nil {
		return nil, err
	}

	// Our log streamer works off a buffer of output
	r.output = &process.Buffer{}
	var outputWriter io.Writer = r.output

	// {stdout, stderr} -> processWriter	// processWriter = io.MultiWriter(allWriters...)
	var allWriters []io.Writer

	// if agent config "EnableJobLogTmpfile" is set, we extend the outputWriter to write to a temporary file.
	// By default, the tmp file will be created on os.TempDir unless config "JobLogPath" is specified.
	// BUILDKITE_JOB_LOG_TMPFILE is an environment variable that contains the full path to this temporary file.
	var tmpFile *os.File
	if conf.AgentConfiguration.EnableJobLogTmpfile {
		jobLogDir := ""
		if conf.AgentConfiguration.JobLogPath != "" {
			jobLogDir = conf.AgentConfiguration.JobLogPath
			r.agentLogger.Debug("[JobRunner] Job Log Path: %s", jobLogDir)
		}
		tmpFile, err = os.CreateTemp(jobLogDir, "buildkite_job_log")
		if err != nil {
			return nil, err
		}
		os.Setenv("BUILDKITE_JOB_LOG_TMPFILE", tmpFile.Name())
		outputWriter = io.MultiWriter(outputWriter, tmpFile)
	}

	pr, pw := io.Pipe()

	switch {
	case conf.AgentConfiguration.ANSITimestamps:
		// processWriter -> prefixer -> outputWriter

		// If we have ansi-timestamps, we can skip line timestamps AND header times
		// this is the future of timestamping
		prefixer := process.NewTimestamper(outputWriter, core.BKTimestamp, 1*time.Second)
		allWriters = append(allWriters, prefixer)

	case conf.AgentConfiguration.TimestampLines:
		// processWriter -> pw -> pr -> process.Scanner -> {headerTimesStreamer, outputWriter}

		// If we have timestamp lines on, we have to buffer lines before we flush them
		// because we need to know if the line is a header or not. It's a bummer.
		allWriters = append(allWriters, pw)

		go func() {
			// Use a scanner to process output line by line
			err := process.NewScanner(r.agentLogger).ScanLines(pr, func(line string) {
				// Send to our header streamer and determine if it's a header
				// or header expansion.
				isHeaderOrExpansion := r.headerTimesStreamer.Scan(line)

				// Prefix non-header log lines with timestamps
				if !isHeaderOrExpansion {
					line = fmt.Sprintf("[%s] %s", time.Now().UTC().Format(time.RFC3339), line)
				}

				// Write the log line to the buffer
				_, _ = outputWriter.Write([]byte(line + "\n"))
			})
			if err != nil {
				r.agentLogger.Error("[JobRunner] Encountered error %v", err)
			}
		}()

	default:
		// processWriter -> {pw, outputWriter};
		// pw -> pr -> process.Scanner -> headerTimesStreamer

		// Write output directly to the line buffer
		allWriters = append(allWriters, pw, outputWriter)

		// Use a scanner to process output for headers only
		go func() {
			err := process.NewScanner(r.agentLogger).ScanLines(pr, func(line string) {
				r.headerTimesStreamer.Scan(line)
			})
			if err != nil {
				r.agentLogger.Error("[JobRunner] Encountered error %v", err)
			}
		}()
	}

	if conf.AgentConfiguration.WriteJobLogsToStdout {
		if conf.AgentConfiguration.LogFormat == "json" {
			log := newJobLogger(
				conf.AgentStdout, logger.StringField("org", r.conf.Job.Env["BUILDKITE_ORGANIZATION_SLUG"]),
				logger.StringField("pipeline", r.conf.Job.Env["BUILDKITE_PIPELINE_SLUG"]),
				logger.StringField("branch", r.conf.Job.Env["BUILDKITE_BRANCH"]),
				logger.StringField("queue", r.conf.Job.Env["BUILDKITE_AGENT_META_DATA_QUEUE"]),
				logger.StringField("build_id", r.conf.Job.Env["BUILDKITE_BUILD_ID"]),
				logger.StringField("build_number", r.conf.Job.Env["BUILDKITE_BUILD_NUMBER"]),
				logger.StringField("job_url", fmt.Sprintf("%s#%s", r.conf.Job.Env["BUILDKITE_BUILD_URL"], r.conf.Job.ID)),
				logger.StringField("job_id", r.conf.Job.ID),
				logger.StringField("step_key", r.conf.Job.Env["BUILDKITE_STEP_KEY"]),
			)
			allWriters = append(allWriters, log)
		} else {
			allWriters = append(allWriters, conf.AgentStdout)
		}
	}

	// The writer that output from the process goes into
	r.jobLogs = io.MultiWriter(allWriters...)

	// Copy the current processes ENV and merge in the new ones. We do this
	// so the sub process gets PATH and stuff. We merge our path in over
	// the top of the current one so the ENV from Buildkite and the agent
	// take precedence over the agent
	processEnv := append(os.Environ(), env...)

	// The process that will run the bootstrap script
	if conf.KubernetesExec {
		// Thank you Mario, but our bootstrap is in another container
		containerCount, err := strconv.Atoi(os.Getenv("BUILDKITE_CONTAINER_COUNT"))
		if err != nil {
			return nil, fmt.Errorf("failed to parse BUILDKITE_CONTAINER_COUNT: %w", err)
		}
		r.process = kubernetes.New(r.agentLogger, kubernetes.Config{
			Stdout:      r.jobLogs,
			Stderr:      r.jobLogs,
			ClientCount: containerCount,
			Env:         processEnv,
		})
	} else { // not Kubernetes
		// The bootstrap-script gets parsed based on the operating system
		cmd, err := shellwords.Split(conf.AgentConfiguration.BootstrapScript)
		if err != nil {
			return nil, fmt.Errorf("splitting bootstrap-script (%q) into tokens: %w", conf.AgentConfiguration.BootstrapScript, err)
		}

		r.process = process.New(r.agentLogger, process.Config{
			Path:              cmd[0],
			Args:              cmd[1:],
			Dir:               conf.AgentConfiguration.BuildPath,
			Env:               processEnv,
			PTY:               conf.AgentConfiguration.RunInPty,
			Stdout:            r.jobLogs,
			Stderr:            r.jobLogs,
			InterruptSignal:   conf.CancelSignal,
			SignalGracePeriod: conf.AgentConfiguration.SignalGracePeriod,
		})
	}

	// Close the writer end of the pipe when the process finishes
	go func() {
		<-r.process.Done()
		if err := pw.Close(); err != nil {
			r.agentLogger.Error("%v", err)
		}
		if tmpFile != nil {
			if err := os.Remove(tmpFile.Name()); err != nil {
				r.agentLogger.Error("%v", err)
			}
		}
	}()

	return r, nil
}

func (r *JobRunner) normalizeVerificationBehavior(behavior string) (string, error) {
	switch behavior {
	case VerificationBehaviourBlock, VerificationBehaviourWarn:
		return behavior, nil
	case "":
		return VerificationBehaviourBlock, nil
	default:
		return "", fmt.Errorf("invalid job verification behavior: %q", behavior)
	}
}

// Creates the environment variables that will be used in the process and writes a flat environment file
func (r *JobRunner) createEnvironment(ctx context.Context) ([]string, error) {
	// Create a clone of our jobs environment. We'll then set the
	// environment variables provided by the agent, which will override any
	// sent by Buildkite. The variables below should always take
	// precedence.
	env := make(map[string]string)
	for key, value := range r.conf.Job.Env {
		env[key] = value
	}

	// The agent registration token should never make it into the job environment
	delete(env, "BUILDKITE_AGENT_TOKEN")

	// Write out the job environment to file:
	// - envShellFile: in k="v" format, with newlines escaped
	// - envJSONFile: as a single JSON object {"k":"v",...}, escaped appropriately for JSON.
	// We present only the clean environment - i.e only variables configured
	// on the job upstream - and expose the path in another environment variable.
	if r.envShellFile != nil {
		for key, value := range env {
			if _, err := r.envShellFile.WriteString(fmt.Sprintf("%s=%q\n", key, value)); err != nil {
				return nil, err
			}
		}
		if err := r.envShellFile.Close(); err != nil {
			return nil, err
		}
	}
	if r.envJSONFile != nil {
		if err := json.NewEncoder(r.envJSONFile).Encode(env); err != nil {
			return nil, err
		}
		if err := r.envJSONFile.Close(); err != nil {
			return nil, err
		}
	}
	// Now that the env files have been written, we can add their corresponding
	// paths to the job env.
	if r.envShellFile != nil {
		env["BUILDKITE_ENV_FILE"] = r.envShellFile.Name()
	}
	if r.envJSONFile != nil {
		env["BUILDKITE_ENV_JSON_FILE"] = r.envJSONFile.Name()
	}

	var ignoredEnv []string

	// Check if the user has defined any protected env
	for k := range ProtectedEnv {
		if _, exists := r.conf.Job.Env[k]; exists {
			ignoredEnv = append(ignoredEnv, k)
		}
	}

	cache := r.conf.Job.Step.Cache
	if cache != nil && len(cache.Paths) > 0 {
		env["BUILDKITE_AGENT_CACHE_PATHS"] = strings.Join(cache.Paths, ",")
	}

	// Set BUILDKITE_IGNORED_ENV so the bootstrap can show warnings
	if len(ignoredEnv) > 0 {
		env["BUILDKITE_IGNORED_ENV"] = strings.Join(ignoredEnv, ",")
	}

	// Add the API configuration
	apiConfig := r.apiClient.Config()
	env["BUILDKITE_AGENT_ENDPOINT"] = apiConfig.Endpoint
	env["BUILDKITE_AGENT_ACCESS_TOKEN"] = apiConfig.Token
	env["BUILDKITE_NO_HTTP2"] = fmt.Sprint(apiConfig.DisableHTTP2)

	// Add agent environment variables
	env["BUILDKITE_AGENT_DEBUG"] = fmt.Sprint(r.conf.Debug)
	env["BUILDKITE_AGENT_DEBUG_HTTP"] = fmt.Sprint(r.conf.DebugHTTP)
	env["BUILDKITE_AGENT_PID"] = strconv.Itoa(os.Getpid())

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
	env["BUILDKITE_GIT_MIRRORS_SKIP_UPDATE"] = fmt.Sprint(r.conf.AgentConfiguration.GitMirrorsSkipUpdate)
	env["BUILDKITE_HOOKS_PATH"] = r.conf.AgentConfiguration.HooksPath
	env["BUILDKITE_PLUGINS_PATH"] = r.conf.AgentConfiguration.PluginsPath
	env["BUILDKITE_SSH_KEYSCAN"] = fmt.Sprint(r.conf.AgentConfiguration.SSHKeyscan)
	env["BUILDKITE_GIT_SUBMODULES"] = fmt.Sprint(r.conf.AgentConfiguration.GitSubmodules)
	env["BUILDKITE_COMMAND_EVAL"] = fmt.Sprint(r.conf.AgentConfiguration.CommandEval)
	env["BUILDKITE_PLUGINS_ENABLED"] = fmt.Sprint(r.conf.AgentConfiguration.PluginsEnabled)
	env["BUILDKITE_LOCAL_HOOKS_ENABLED"] = fmt.Sprint(r.conf.AgentConfiguration.LocalHooksEnabled)
	env["BUILDKITE_GIT_CHECKOUT_FLAGS"] = r.conf.AgentConfiguration.GitCheckoutFlags
	env["BUILDKITE_GIT_CLONE_FLAGS"] = r.conf.AgentConfiguration.GitCloneFlags
	env["BUILDKITE_GIT_FETCH_FLAGS"] = r.conf.AgentConfiguration.GitFetchFlags
	env["BUILDKITE_GIT_CLONE_MIRROR_FLAGS"] = r.conf.AgentConfiguration.GitCloneMirrorFlags
	env["BUILDKITE_GIT_CLEAN_FLAGS"] = r.conf.AgentConfiguration.GitCleanFlags
	env["BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT"] = strconv.Itoa(r.conf.AgentConfiguration.GitMirrorsLockTimeout)
	env["BUILDKITE_SHELL"] = r.conf.AgentConfiguration.Shell
	env["BUILDKITE_AGENT_EXPERIMENT"] = strings.Join(experiments.Enabled(ctx), ",")
	env["BUILDKITE_REDACTED_VARS"] = strings.Join(r.conf.AgentConfiguration.RedactedVars, ",")
	env["BUILDKITE_STRICT_SINGLE_HOOKS"] = fmt.Sprint(r.conf.AgentConfiguration.StrictSingleHooks)
	env["BUILDKITE_CANCEL_GRACE_PERIOD"] = strconv.Itoa(r.conf.AgentConfiguration.CancelGracePeriod)
	env["BUILDKITE_SIGNAL_GRACE_PERIOD_SECONDS"] = strconv.Itoa(int(r.conf.AgentConfiguration.SignalGracePeriod / time.Second))
	env["BUILDKITE_TRACE_CONTEXT_ENCODING"] = r.conf.AgentConfiguration.TraceContextEncoding

	if r.conf.KubernetesExec {
		env["BUILDKITE_KUBERNETES_EXEC"] = "true"
	}

	// propagate CancelSignal to bootstrap, unless it's the default SIGTERM
	if r.conf.CancelSignal != process.SIGTERM {
		env["BUILDKITE_CANCEL_SIGNAL"] = r.conf.CancelSignal.String()
	}

	// Whether to enable profiling in the bootstrap
	if r.conf.AgentConfiguration.Profile != "" {
		env["BUILDKITE_AGENT_PROFILE"] = r.conf.AgentConfiguration.Profile
	}

	// PTY-mode is enabled by default in `start` and `bootstrap`, so we only need
	// to propagate it if it's explicitly disabled.
	if !r.conf.AgentConfiguration.RunInPty {
		env["BUILDKITE_PTY"] = "false"
	}

	// pass through the KMS key ID for signing
	if r.conf.AgentConfiguration.SigningAWSKMSKey != "" {
		env["BUILDKITE_AGENT_AWS_KMS_KEY"] = r.conf.AgentConfiguration.SigningAWSKMSKey
	}

	// Pass signing details through to the executor - any pipelines uploaded by this agent will be signed
	if r.conf.AgentConfiguration.SigningJWKSFile != "" {
		env["BUILDKITE_AGENT_JWKS_FILE"] = r.conf.AgentConfiguration.SigningJWKSFile
	}

	if r.conf.AgentConfiguration.SigningJWKSKeyID != "" {
		env["BUILDKITE_AGENT_JWKS_KEY_ID"] = r.conf.AgentConfiguration.SigningJWKSKeyID
	}

	if r.conf.AgentConfiguration.DebugSigning {
		env["BUILDKITE_AGENT_DEBUG_SIGNING"] = "true"
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
	env["BUILDKITE_PLUGIN_VALIDATION"] = fmt.Sprint(enablePluginValidation)

	if r.conf.AgentConfiguration.TracingBackend != "" {
		env["BUILDKITE_TRACING_BACKEND"] = r.conf.AgentConfiguration.TracingBackend
		env["BUILDKITE_TRACING_SERVICE_NAME"] = r.conf.AgentConfiguration.TracingServiceName
	}

	env["BUILDKITE_AGENT_DISABLE_WARNINGS_FOR"] = strings.Join(r.conf.AgentConfiguration.DisableWarningsFor, ",")

	// see documentation for BuildkiteMessageMax
	if err := truncateEnv(r.agentLogger, env, BuildkiteMessageName, BuildkiteMessageMax); err != nil {
		r.agentLogger.Warn("failed to truncate %s: %v", BuildkiteMessageName, err)
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

func (r *JobRunner) executePreBootstrapHook(ctx context.Context, hook string) (bool, error) {
	r.agentLogger.Info("Running pre-bootstrap hook %q", hook)

	sh, err := shell.New()
	if err != nil {
		return false, err
	}

	// This (plus inherited) is the only ENV that should be exposed
	// to the pre-bootstrap hook.
	// - Env files are designed to be validated by the pre-bootstrap hook
	// - The pre-bootstrap hook may want to create annotations, so it can also
	//   have a few necessary and global args as env vars.
	sh.Env.Set("BUILDKITE_ENV_FILE", r.envShellFile.Name())
	sh.Env.Set("BUILDKITE_ENV_JSON_FILE", r.envJSONFile.Name())
	apiConfig := r.apiClient.Config()
	sh.Env.Set("BUILDKITE_JOB_ID", r.conf.Job.ID)
	sh.Env.Set("BUILDKITE_AGENT_ACCESS_TOKEN", apiConfig.Token)
	sh.Env.Set("BUILDKITE_AGENT_ENDPOINT", apiConfig.Endpoint)
	sh.Env.Set("BUILDKITE_NO_HTTP2", fmt.Sprint(apiConfig.DisableHTTP2))
	sh.Env.Set("BUILDKITE_AGENT_DEBUG", fmt.Sprint(r.conf.Debug))
	sh.Env.Set("BUILDKITE_AGENT_DEBUG_HTTP", fmt.Sprint(r.conf.DebugHTTP))

	sh.Writer = LogWriter{
		l: r.agentLogger,
	}

	if err := sh.RunScript(ctx, hook, nil); err != nil {
		r.agentLogger.Error("Finished pre-bootstrap hook %q: job rejected: %s", hook, err)
		return false, err
	}

	r.agentLogger.Info("Finished pre-bootstrap hook %q: job accepted", hook)
	return true, nil
}

// jobCancellationChecker waits for the processes to start, then continuously
// polls GetJobState to see if the job has been cancelled server-side. If so,
// it calls r.Cancel.
func (r *JobRunner) jobCancellationChecker(ctx context.Context, wg *sync.WaitGroup) {
	ctx, setStat, done := status.AddSimpleItem(ctx, "Job Cancellation Checker")
	defer done()
	setStat("Starting...")

	defer func() {
		// Mark this routine as done in the wait group
		wg.Done()

		r.agentLogger.Debug("[JobRunner] Routine that refreshes the job has finished")
	}()

	select {
	case <-r.process.Started():
	case <-ctx.Done():
		return
	}

	for {
		setStat("📡 Fetching job state from Buildkite")

		// Re-get the job and check its status to see if it's been cancelled
		jobState, response, err := r.apiClient.GetJobState(ctx, r.conf.Job.ID)

		if err != nil {
			if response != nil && response.StatusCode == 401 {
				r.agentLogger.Error("Invalid access token, cancelling job %s", r.conf.Job.ID)
				if err := r.Cancel(); err != nil {
					r.agentLogger.Error("Failed to cancel the process (job: %s): %v", r.conf.Job.ID, err)
				}
			} else {
				// We don't really care if it fails, we'll just try again soon anyway
				r.agentLogger.Warn("Problem with getting job state %s (%s)", r.conf.Job.ID, err)
			}
		} else if jobState.State == "canceling" || jobState.State == "canceled" {
			if err := r.Cancel(); err != nil {
				r.agentLogger.Error("Unexpected error canceling process as requested by server (job: %s) (err: %s)", r.conf.Job.ID, err)
			}
		}

		setStat("😴 Sleeping for a bit")

		// Sleep for a bit, or until the job is finished
		select {
		case <-time.After(r.conf.JobStatusInterval):
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
		response, err := r.apiClient.SaveHeaderTimes(ctx, r.conf.Job.ID, &api.HeaderTimes{Times: times})
		if err != nil {
			if response != nil && (response.StatusCode >= 400 && response.StatusCode <= 499) {
				r.agentLogger.Warn("Buildkite rejected the header times (%s)", err)
				retrier.Break()
			} else {
				r.agentLogger.Warn("%s (%s)", err, retrier)
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
