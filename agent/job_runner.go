package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/core"
	envutil "github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/internal/shell"
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

	// KubernetesExec enables Kubernetes execution mode. When true, the job runner
	// creates a kubernetes.Runner that listens on a UNIX socket for other agent containers
	// to connect, rather than spawning a local bootstrap subprocess. The other agent containers
	// containers run `kubernetes-bootstrap` which connects to this socket, receives
	// environment variables, and executes the bootstrap phases.
	KubernetesExec bool

	// Stdout of the parent agent process. Used for job log stdout writing arg, for simpler containerized log collection.
	AgentStdout io.Writer
}

type JobRunner struct {
	// The configuration for the job runner
	conf JobRunnerConfig

	// How the JobRunner should respond when job verification fails (one of `block` or `warn`)
	VerificationFailureBehavior string

	// agentLogger is a agentLogger that outputs to the agent logs
	agentLogger logger.Logger

	// The APIClient that will be used when updating the job
	apiClient *api.Client

	// The agentlib Client is used to drive some APIClient methods
	client *core.Client

	// The internal process of the job
	process jobProcess

	// The internal buffer of the process output
	output *process.Buffer

	// The internal header time streamer
	headerTimesStreamer *headerTimesStreamer

	// The internal log streamer. Don't write to this directly, use `jobLogs` instead
	logStreamer *LogStreamer

	// jobLogs is an io.Writer that sends data to the job logs
	jobLogs io.Writer

	// Job cancellation control
	cancelLock sync.Mutex // prevent concurrent calls to Cancel

	// State flags
	cancelled     atomic.Bool // job is cancelled?
	agentStopping atomic.Bool

	// When the job was started
	startedAt time.Time

	// Files containing a copy of the job env
	envShellFile *os.File
	envJSONFile  *os.File
}

// jobProcess is either a *process.Process, or a *kubernetes.Runner.
type jobProcess interface {
	Done() <-chan struct{}
	Started() <-chan struct{}
	Interrupt() error
	Terminate() error
	Run(ctx context.Context) error
	WaitStatus() process.WaitStatus
}

// Initializes the job runner
func NewJobRunner(ctx context.Context, l logger.Logger, apiClient *api.Client, conf JobRunnerConfig) (*JobRunner, error) {
	// If the accept response has a token attached, we should use that instead of the Agent Access Token that
	// our current apiClient is using
	if conf.Job.Token != "" {
		clientConf := apiClient.Config()
		clientConf.Token = conf.Job.Token
		apiClient = apiClient.New(clientConf)
	}

	r := &JobRunner{
		agentLogger: l,
		conf:        conf,
		apiClient:   apiClient,
		client:      &core.Client{APIClient: apiClient, Logger: l},
	}

	var err error
	r.VerificationFailureBehavior, err = r.normalizeVerificationBehavior(conf.AgentConfiguration.VerificationFailureBehaviour)
	if err != nil {
		return nil, fmt.Errorf("setting no signature behavior: %w", err)
	}

	if conf.JobStatusInterval == 0 {
		conf.JobStatusInterval = 1 * time.Second
	}

	// Create our header times struct
	r.headerTimesStreamer = newHeaderTimesStreamer(r.agentLogger, r.onUploadHeaderTime)

	// The log streamer that will take the output chunks, and send them to
	// the Buildkite Agent API
	r.logStreamer = NewLogStreamer(
		r.agentLogger,
		func(ctx context.Context, chunk *api.Chunk) error {
			startUpload := time.Now()
			// core.Client.UploadChunk contains the retry/backoff.
			if err := r.client.UploadChunk(ctx, r.conf.Job.ID, chunk); err != nil {
				logChunkUploadErrors.Inc()
				logBytesUploadErrors.Add(float64(chunk.Size))
				return err
			}
			logUploadDurations.Observe(time.Since(startUpload).Seconds())
			logChunksUploaded.Inc()
			logBytesUploaded.Add(float64(chunk.Size))
			return nil
		},
		LogStreamerConfig{
			Concurrency:       3,
			MaxChunkSizeBytes: r.conf.Job.ChunksMaxSizeBytes,
			MaxSizeBytes:      r.conf.Job.LogMaxSizeBytes,
		},
	)

	r.envShellFile, r.envJSONFile, err = createJobEnvFiles(r.agentLogger, r.conf.Job.ID, conf.KubernetesExec)
	if err != nil {
		return nil, err
	}

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

		err := os.Chmod(tmpFile.Name(), 0o644) // Make it world-readable - useful for log collection etc
		if err != nil {
			return nil, fmt.Errorf("failed to set permissions on job log tmpfile %s: %w", tmpFile.Name(), err)
		}

		if err := os.Setenv("BUILDKITE_JOB_LOG_TMPFILE", tmpFile.Name()); err != nil {
			return nil, fmt.Errorf("failed to set BUILDKITE_JOB_LOG_TMPFILE: %v", err)
		}
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
				logger.StringField("build_url", r.conf.Job.Env["BUILDKITE_BUILD_URL"]),
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
		r.process = kubernetes.NewRunner(r.agentLogger, kubernetes.RunnerConfig{
			Stdout:             r.jobLogs,
			Stderr:             r.jobLogs,
			ClientCount:        containerCount,
			Env:                processEnv,
			ClientStartTimeout: 5 * time.Minute,
			ClientLostTimeout:  30 * time.Second,
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
	maps.Copy(env, r.conf.Job.Env)

	// The agent registration token should never make it into the job environment
	delete(env, "BUILDKITE_AGENT_TOKEN")

	// When in KubernetesExec mode, filter out the Kubernetes plugin,
	// since it's not a real plugin. agent-stack-k8s reads it but we have no
	// need for it. Supplying it when not using agent-stack-k8s is a mistake
	// but not one worth preventing.
	if pluginsJSON := env["BUILDKITE_PLUGINS"]; pluginsJSON != "" && r.conf.KubernetesExec {
		filtered, err := removeKubernetesPlugin([]byte(pluginsJSON))
		if err != nil {
			r.agentLogger.Error("Invalid BUILDKITE_PLUGINS: %w", err)
		}
		if string(filtered) == "" {
			delete(env, "BUILDKITE_PLUGINS")
		} else {
			env["BUILDKITE_PLUGINS"] = string(filtered)
		}
	}

	// Wrap setting values in env, so that when any that were already present in
	// supplied Job env are overwritten, they can be added to ignoredEnv.
	var ignoredEnv []string
	setEnv := func(name, value string) {
		if _, exists := env[name]; exists {
			ignoredEnv = append(ignoredEnv, name)
		}
		env[name] = value
	}

	// Write out the job environment to file:
	// - envShellFile: in k="v" format, with newlines escaped. If the
	//   propagate-agent-vars experiment is enabled, the names of several agent
	//   config variables are prepended at the top.
	// - envJSONFile: as a single JSON object {"k":"v",...}, escaped appropriately for JSON.
	// We present only the clean environment - i.e only variables configured
	// on the job upstream - and expose the path in another environment variable.
	if r.envShellFile != nil {
		if experiments.IsEnabled(ctx, experiments.PropagateAgentConfigVars) {
			// Note that some variables in this list might not be defined later,
			// when something comes to read the file. See below where they are
			// added conditionally, e.g. BUILDKITE_TRACING_BACKEND.
			// Docker in particular tolerates undefined vars in an env file
			// without complaints.
			const agentCfgVars = `BUILDKITE_GIT_CHECKOUT_FLAGS
BUILDKITE_GIT_CLEAN_FLAGS
BUILDKITE_GIT_CLONE_FLAGS
BUILDKITE_GIT_CLONE_MIRROR_FLAGS
BUILDKITE_GIT_FETCH_FLAGS
BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT
BUILDKITE_GIT_MIRRORS_PATH
BUILDKITE_GIT_MIRRORS_SKIP_UPDATE
BUILDKITE_GIT_SUBMODULES
BUILDKITE_CANCEL_GRACE_PERIOD
BUILDKITE_COMMAND_EVAL
BUILDKITE_LOCAL_HOOKS_ENABLED
BUILDKITE_PLUGINS_ENABLED
BUILDKITE_REDACTED_VARS
BUILDKITE_SHELL
BUILDKITE_SIGNAL_GRACE_PERIOD_SECONDS
BUILDKITE_SSH_KEYSCAN
BUILDKITE_STRICT_SINGLE_HOOKS
BUILDKITE_TRACE_CONTEXT_ENCODING
BUILDKITE_TRACING_BACKEND
BUILDKITE_TRACING_SERVICE_NAME
BUILDKITE_TRACING_TRACEPARENT
BUILDKITE_TRACING_PROPAGATE_TRACEPARENT
BUILDKITE_AGENT_AWS_KMS_KEY
BUILDKITE_AGENT_JWKS_FILE
BUILDKITE_AGENT_JWKS_KEY_ID`
			if _, err := fmt.Fprintln(r.envShellFile, agentCfgVars); err != nil {
				return nil, err
			}
		}

		for key, value := range env {
			if _, err := fmt.Fprintf(r.envShellFile, "%s=%q\n", key, value); err != nil {
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
		setEnv("BUILDKITE_ENV_FILE", r.envShellFile.Name())
	}
	if r.envJSONFile != nil {
		setEnv("BUILDKITE_ENV_JSON_FILE", r.envJSONFile.Name())
	}

	cache := r.conf.Job.Step.Cache
	if cache != nil && len(cache.Paths) > 0 {
		setEnv("BUILDKITE_AGENT_CACHE_PATHS", strings.Join(cache.Paths, ","))
	}

	// Set BUILDKITE_SECRETS_CONFIG so bootstrap can access secrets configuration
	if len(r.conf.Job.Step.Secrets) > 0 {
		secretsJSON, err := json.Marshal(r.conf.Job.Step.Secrets)
		if err != nil {
			r.agentLogger.Error("Failed to marshal secrets configuration: %v", err)
			return nil, err
		}

		setEnv("BUILDKITE_SECRETS_CONFIG", string(secretsJSON))
	}

	// Add the API configuration
	apiConfig := r.apiClient.Config()
	setEnv("BUILDKITE_AGENT_ENDPOINT", apiConfig.Endpoint)
	setEnv("BUILDKITE_AGENT_ACCESS_TOKEN", apiConfig.Token)
	setEnv("BUILDKITE_NO_HTTP2", fmt.Sprint(apiConfig.DisableHTTP2))

	// ... including any server-specified request headers, so that sub-processes such as
	// buildkite-agent annotate etc can respect them.
	for header, values := range r.apiClient.ServerSpecifiedRequestHeaders() {
		k := fmt.Sprintf(
			"BUILDKITE_REQUEST_HEADER_%s",
			strings.ToUpper(strings.ReplaceAll(header, "-", "_")),
		)
		for _, v := range values {
			env[k] = v
		}
	}

	// Add agent environment variables
	setEnv("BUILDKITE_AGENT_DEBUG", fmt.Sprint(r.conf.Debug))
	setEnv("BUILDKITE_AGENT_DEBUG_HTTP", fmt.Sprint(r.conf.DebugHTTP))
	setEnv("BUILDKITE_AGENT_PID", strconv.Itoa(os.Getpid()))

	// We know the BUILDKITE_BIN_PATH dir, because it's the path to the
	// currently running file (there is only 1 binary)
	// Note that [os.Executable] returns an absolute path.
	exePath, err := os.Executable()
	if err != nil {
		return nil, err
	}

	setEnv("BUILDKITE_BIN_PATH", filepath.Dir(exePath))

	// Add options from the agent configuration
	setEnv("BUILDKITE_CONFIG_PATH", r.conf.AgentConfiguration.ConfigPath)
	setEnv("BUILDKITE_BUILD_PATH", r.conf.AgentConfiguration.BuildPath)
	setEnv("BUILDKITE_SOCKETS_PATH", r.conf.AgentConfiguration.SocketsPath)
	setEnv("BUILDKITE_GIT_MIRRORS_PATH", r.conf.AgentConfiguration.GitMirrorsPath)
	setEnv("BUILDKITE_GIT_MIRRORS_SKIP_UPDATE", fmt.Sprint(r.conf.AgentConfiguration.GitMirrorsSkipUpdate))
	setEnv("BUILDKITE_HOOKS_PATH", r.conf.AgentConfiguration.HooksPath)
	setEnv("BUILDKITE_ADDITIONAL_HOOKS_PATHS", strings.Join(r.conf.AgentConfiguration.AdditionalHooksPaths, ","))
	setEnv("BUILDKITE_PLUGINS_PATH", r.conf.AgentConfiguration.PluginsPath)
	setEnv("BUILDKITE_SSH_KEYSCAN", fmt.Sprint(r.conf.AgentConfiguration.SSHKeyscan))
	setEnv("BUILDKITE_GIT_SUBMODULES", fmt.Sprint(r.conf.AgentConfiguration.GitSubmodules))
	setEnv("BUILDKITE_COMMAND_EVAL", fmt.Sprint(r.conf.AgentConfiguration.CommandEval))
	setEnv("BUILDKITE_PLUGINS_ENABLED", fmt.Sprint(r.conf.AgentConfiguration.PluginsEnabled))
	// Allow BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH to be enabled either by config
	// or by pipeline/step env.
	if r.conf.AgentConfiguration.PluginsAlwaysCloneFresh {
		setEnv("BUILDKITE_PLUGINS_ALWAYS_CLONE_FRESH", "true")
	}
	setEnv("BUILDKITE_LOCAL_HOOKS_ENABLED", fmt.Sprint(r.conf.AgentConfiguration.LocalHooksEnabled))

	setEnv("BUILDKITE_GIT_CHECKOUT_FLAGS", r.conf.AgentConfiguration.GitCheckoutFlags)
	setEnv("BUILDKITE_GIT_CLONE_FLAGS", r.conf.AgentConfiguration.GitCloneFlags)
	setEnv("BUILDKITE_GIT_FETCH_FLAGS", r.conf.AgentConfiguration.GitFetchFlags)
	setEnv("BUILDKITE_GIT_CLONE_MIRROR_FLAGS", r.conf.AgentConfiguration.GitCloneMirrorFlags)
	setEnv("BUILDKITE_GIT_CLEAN_FLAGS", r.conf.AgentConfiguration.GitCleanFlags)
	setEnv("BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT", strconv.Itoa(r.conf.AgentConfiguration.GitMirrorsLockTimeout))

	setEnv("BUILDKITE_SHELL", r.conf.AgentConfiguration.Shell)
	setEnv("BUILDKITE_AGENT_EXPERIMENT", strings.Join(experiments.Enabled(ctx), ","))
	setEnv("BUILDKITE_REDACTED_VARS", strings.Join(r.conf.AgentConfiguration.RedactedVars, ","))
	setEnv("BUILDKITE_STRICT_SINGLE_HOOKS", fmt.Sprint(r.conf.AgentConfiguration.StrictSingleHooks))
	setEnv("BUILDKITE_CANCEL_GRACE_PERIOD", strconv.Itoa(r.conf.AgentConfiguration.CancelGracePeriod))
	setEnv("BUILDKITE_SIGNAL_GRACE_PERIOD_SECONDS", strconv.Itoa(int(r.conf.AgentConfiguration.SignalGracePeriod/time.Second)))
	setEnv("BUILDKITE_TRACE_CONTEXT_ENCODING", r.conf.AgentConfiguration.TraceContextEncoding)

	if !r.conf.AgentConfiguration.AllowMultipartArtifactUpload {
		setEnv("BUILDKITE_NO_MULTIPART_ARTIFACT_UPLOAD", "true")
	}

	// propagate CancelSignal to bootstrap, unless it's the default SIGTERM
	if r.conf.CancelSignal != process.SIGTERM {
		setEnv("BUILDKITE_CANCEL_SIGNAL", r.conf.CancelSignal.String())
	}

	// Whether to enable profiling in the bootstrap
	if r.conf.AgentConfiguration.Profile != "" {
		setEnv("BUILDKITE_AGENT_PROFILE", r.conf.AgentConfiguration.Profile)
	}

	// PTY-mode is enabled by default in `start` and `bootstrap`, so we only need
	// to propagate it if it's explicitly disabled.
	if !r.conf.AgentConfiguration.RunInPty {
		setEnv("BUILDKITE_PTY", "false")
	}

	// pass through the KMS key ID for signing
	if r.conf.AgentConfiguration.SigningAWSKMSKey != "" {
		setEnv("BUILDKITE_AGENT_AWS_KMS_KEY", r.conf.AgentConfiguration.SigningAWSKMSKey)
	}

	// Pass signing details through to the executor - any pipelines uploaded by this agent will be signed
	if r.conf.AgentConfiguration.SigningJWKSFile != "" {
		setEnv("BUILDKITE_AGENT_JWKS_FILE", r.conf.AgentConfiguration.SigningJWKSFile)
	}

	if r.conf.AgentConfiguration.SigningJWKSKeyID != "" {
		setEnv("BUILDKITE_AGENT_JWKS_KEY_ID", r.conf.AgentConfiguration.SigningJWKSKeyID)
	}

	if r.conf.AgentConfiguration.DebugSigning {
		setEnv("BUILDKITE_AGENT_DEBUG_SIGNING", "true")
	}

	enablePluginValidation := r.conf.AgentConfiguration.PluginValidation
	// Allow BUILDKITE_PLUGIN_VALIDATION to be enabled from env for easier
	// per-pipeline testing
	if pluginValidation, ok := env["BUILDKITE_PLUGIN_VALIDATION"]; ok {
		switch pluginValidation {
		case "true", "1", "on":
			// Skip ignoredEnv by pretending it wasn't set by the job.
			delete(env, "BUILDKITE_PLUGIN_VALIDATION")
			enablePluginValidation = true
		}
	}
	setEnv("BUILDKITE_PLUGIN_VALIDATION", fmt.Sprint(enablePluginValidation))

	if r.conf.AgentConfiguration.TracingBackend != "" {
		setEnv("BUILDKITE_TRACING_BACKEND", r.conf.AgentConfiguration.TracingBackend)
		setEnv("BUILDKITE_TRACING_SERVICE_NAME", r.conf.AgentConfiguration.TracingServiceName)

		// Buildkite backend can provide a traceparent property on the job
		// which can be propagated to the job tracing if OpenTelemetry is used
		//
		// https://www.w3.org/TR/trace-context/#traceparent-header
		if r.conf.Job.TraceParent != "" {
			setEnv("BUILDKITE_TRACING_TRACEPARENT", r.conf.Job.TraceParent)
		}
		if r.conf.AgentConfiguration.TracingPropagateTraceparent {
			setEnv("BUILDKITE_TRACING_PROPAGATE_TRACEPARENT", "true")
		}
	}

	setEnv("BUILDKITE_AGENT_DISABLE_WARNINGS_FOR", strings.Join(r.conf.AgentConfiguration.DisableWarningsFor, ","))

	// see documentation for BuildkiteMessageMax
	if err := truncateEnv(r.agentLogger, env, BuildkiteMessageName, BuildkiteMessageMax); err != nil {
		r.agentLogger.Warn("failed to truncate %s: %v", BuildkiteMessageName, err)
		// attempt to continue anyway
	}

	// Finally, set BUILDKITE_IGNORED_ENV so the bootstrap can show warnings.
	if len(ignoredEnv) > 0 {
		env["BUILDKITE_IGNORED_ENV"] = strings.Join(ignoredEnv, ",")
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

func removeKubernetesPlugin(pluginsJSON []byte) ([]byte, error) {
	var plugins []map[string]json.RawMessage
	if err := json.Unmarshal(pluginsJSON, &plugins); err != nil {
		return pluginsJSON, err
	}
	plugins = slices.DeleteFunc(plugins, func(plugin map[string]json.RawMessage) bool {
		_, isK8sPlugin := plugin["github.com/buildkite-plugins/kubernetes-buildkite-plugin"]
		return isK8sPlugin
	})
	return json.Marshal(plugins)
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

	sh, err := shell.New(
		shell.WithStdout(LogWriter{l: r.agentLogger}),
	)
	if err != nil {
		return false, err
	}

	// This (plus inherited) is the only ENV that should be exposed
	// to the pre-bootstrap hook.
	// - Env files are designed to be validated by the pre-bootstrap hook
	// - The pre-bootstrap hook may want to create annotations, so it can also
	//   have a few necessary and global args as env vars.
	environ := envutil.New()
	environ.Set("BUILDKITE_ENV_FILE", r.envShellFile.Name())
	environ.Set("BUILDKITE_ENV_JSON_FILE", r.envJSONFile.Name())
	environ.Set("BUILDKITE_JOB_ID", r.conf.Job.ID)
	apiConfig := r.apiClient.Config()
	environ.Set("BUILDKITE_AGENT_ACCESS_TOKEN", apiConfig.Token)
	environ.Set("BUILDKITE_AGENT_ENDPOINT", apiConfig.Endpoint)
	environ.Set("BUILDKITE_NO_HTTP2", fmt.Sprint(apiConfig.DisableHTTP2))
	environ.Set("BUILDKITE_AGENT_DEBUG", fmt.Sprint(r.conf.Debug))
	environ.Set("BUILDKITE_AGENT_DEBUG_HTTP", fmt.Sprint(r.conf.DebugHTTP))

	script, err := sh.Script(hook)
	if err != nil {
		r.agentLogger.Error("Finished pre-bootstrap hook %q: script not runnable: %v", hook, err)
		return false, err
	}
	if err := script.Run(ctx, shell.ShowPrompt(false), shell.WithExtraEnv(environ)); err != nil {
		r.agentLogger.Error("Finished pre-bootstrap hook %q: job rejected: %v", hook, err)
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

	intervalTicker := time.NewTicker(r.conf.JobStatusInterval)
	defer intervalTicker.Stop()
	first := make(chan struct{}, 1)
	first <- struct{}{}

	for {
		setStat("😴 Waiting for next job status interval tick")
		select {
		case <-first:
			// continue below
		case <-intervalTicker.C:
			// continue below
		case <-ctx.Done():
			return
		case <-r.process.Done():
			return
		}

		// Within the interval, wait a random amount of time to avoid
		// spontaneous synchronisation across agents.
		jitter := rand.N(r.conf.JobStatusInterval)
		setStat(fmt.Sprintf("🫨 Jittering for %v", jitter))
		select {
		case <-time.After(jitter):
			// continue below
		case <-ctx.Done():
			return
		case <-r.process.Done():
			return
		}

		setStat("📡 Fetching job state from Buildkite")

		// Re-get the job and check its status to see if it's been cancelled
		jobState, response, err := r.apiClient.GetJobState(ctx, r.conf.Job.ID)
		if err != nil {
			if response != nil && response.StatusCode == 401 {
				r.agentLogger.Error("Invalid access token, cancelling job %s", r.conf.Job.ID)
				if err := r.Cancel(CancelReasonInvalidToken); err != nil {
					r.agentLogger.Error("Failed to cancel the process (job: %s): %v", r.conf.Job.ID, err)
				}
			} else {
				// We don't really care if it fails, we'll just try again soon anyway
				r.agentLogger.Warn("Problem with getting job state %s (%s)", r.conf.Job.ID, err)
			}
			continue // the loop
		}
		if jobState.State == "canceling" || jobState.State == "canceled" {
			if err := r.Cancel(CancelReasonJobState); err != nil {
				r.agentLogger.Error("Unexpected error canceling process as requested by server (job: %s) (err: %s)", r.conf.Job.ID, err)
			}
		}
	}
}

func (r *JobRunner) onUploadHeaderTime(ctx context.Context, cursor, total int, times map[string]string) {
	err := roko.NewRetrier(
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
	if err != nil {
		r.agentLogger.Error("Ultimately unable to upload header times: %v", err)
	}
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

func createJobEnvFiles(l logger.Logger, jobID string, kubernetesExec bool) (shellFile, jsonFile *os.File, err error) {
	// Use /workspace in Kubernetes mode for shared volume access between containers
	tempDir := os.TempDir()
	if kubernetesExec {
		tempDir = "/workspace"
	}

	// tempDir is not guaranteed to exist
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		// Actual file permissions will be reduced by umask, and won't be 0o777 unless the user has manually changed the umask to 000
		if err = os.MkdirAll(tempDir, 0o777); err != nil {
			return nil, nil, err
		}
	}

	shellFile, err = os.CreateTemp(tempDir, fmt.Sprintf("job-env-%s", jobID))
	if err != nil {
		return nil, nil, err
	}
	l.Debug("[JobRunner] Created env file (shell format): %s", shellFile.Name())

	jsonFile, err = os.CreateTemp(tempDir, fmt.Sprintf("job-env-json-%s", jobID))
	if err != nil {
		shellFile.Close()
		os.Remove(shellFile.Name())
		return nil, nil, err
	}
	l.Debug("[JobRunner] Created env file (JSON format): %s", jsonFile.Name())

	return shellFile, jsonFile, nil
}
