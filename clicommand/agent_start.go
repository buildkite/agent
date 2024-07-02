package clicommand

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/agentapi"
	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/internal/job/hook"
	"github.com/buildkite/agent/v3/internal/job/shell"
	"github.com/buildkite/agent/v3/internal/utils"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/metrics"
	"github.com/buildkite/agent/v3/process"
	"github.com/buildkite/agent/v3/status"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/agent/v3/version"
	"github.com/buildkite/shellwords"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli"
	"golang.org/x/exp/maps"
)

const startDescription = `Usage:

    buildkite-agent start [options...]

Description:

When a job is ready to run it will call the "bootstrap-script"
and pass it all the environment variables required for the job to run.
This script is responsible for checking out the code, and running the
actual build script defined in the pipeline.

The agent will run any jobs within a PTY (pseudo terminal) if available.

Example:

    $ buildkite-agent start --token xxx`

var (
	verificationFailureBehaviors = []string{agent.VerificationBehaviourBlock, agent.VerificationBehaviourWarn}

	buildkiteSetEnvironmentVariables = []*regexp.Regexp{
		regexp.MustCompile("^BUILDKITE$"),
		regexp.MustCompile("^BUILDKITE_.*$"),
		regexp.MustCompile("^CI$"),
	}
)

// Adding config requires changes in a few different spots
// - The AgentStartConfig struct with a cli parameter
// - As a flag in the AgentStartCommand (with matching env)
// - Into an env to be passed to the bootstrap in agent/job_runner.go, createEnvironment()
// - Into clicommand/bootstrap.go to read it from the env into the bootstrap config

type AgentStartConfig struct {
	Config string `cli:"config"`

	Name              string   `cli:"name"`
	Priority          string   `cli:"priority"`
	Spawn             int      `cli:"spawn"`
	SpawnPerCPU       int      `cli:"spawn-per-cpu"`
	SpawnWithPriority bool     `cli:"spawn-with-priority"`
	RedactedVars      []string `cli:"redacted-vars" normalize:"list"`
	CancelSignal      string   `cli:"cancel-signal"`

	SigningJWKSFile  string `cli:"signing-jwks-file" normalize:"filepath"`
	SigningJWKSKeyID string `cli:"signing-jwks-key-id"`
	DebugSigning     bool   `cli:"debug-signing"`

	VerificationJWKSFile        string `cli:"verification-jwks-file" normalize:"filepath"`
	VerificationFailureBehavior string `cli:"verification-failure-behavior"`

	AcquireJob                 string `cli:"acquire-job"`
	DisconnectAfterJob         bool   `cli:"disconnect-after-job"`
	DisconnectAfterIdleTimeout int    `cli:"disconnect-after-idle-timeout"`
	CancelGracePeriod          int    `cli:"cancel-grace-period"`
	SignalGracePeriodSeconds   int    `cli:"signal-grace-period-seconds"`

	EnableJobLogTmpfile bool   `cli:"enable-job-log-tmpfile"`
	JobLogPath          string `cli:"job-log-path" normalize:"filepath"`

	LogFormat            string   `cli:"log-format"`
	WriteJobLogsToStdout bool     `cli:"write-job-logs-to-stdout"`
	DisableWarningsFor   []string `cli:"disable-warnings-for" normalize:"list"`

	BuildPath   string `cli:"build-path" normalize:"filepath" validate:"required"`
	HooksPath   string `cli:"hooks-path" normalize:"filepath"`
	SocketsPath string `cli:"sockets-path" normalize:"filepath"`
	PluginsPath string `cli:"plugins-path" normalize:"filepath"`

	Shell           string `cli:"shell"`
	BootstrapScript string `cli:"bootstrap-script" normalize:"commandpath"`
	NoPTY           bool   `cli:"no-pty"`

	NoANSITimestamps bool `cli:"no-ansi-timestamps"`
	TimestampLines   bool `cli:"timestamp-lines"`

	Queue                     string   `cli:"queue"`
	Tags                      []string `cli:"tags" normalize:"list"`
	TagsFromEC2MetaData       bool     `cli:"tags-from-ec2-meta-data"`
	TagsFromEC2MetaDataPaths  []string `cli:"tags-from-ec2-meta-data-paths" normalize:"list"`
	TagsFromEC2Tags           bool     `cli:"tags-from-ec2-tags"`
	TagsFromECSMetaData       bool     `cli:"tags-from-ecs-meta-data"`
	TagsFromGCPMetaData       bool     `cli:"tags-from-gcp-meta-data"`
	TagsFromGCPMetaDataPaths  []string `cli:"tags-from-gcp-meta-data-paths" normalize:"list"`
	TagsFromGCPLabels         bool     `cli:"tags-from-gcp-labels"`
	TagsFromHost              bool     `cli:"tags-from-host"`
	WaitForEC2TagsTimeout     string   `cli:"wait-for-ec2-tags-timeout"`
	WaitForEC2MetaDataTimeout string   `cli:"wait-for-ec2-meta-data-timeout"`
	WaitForECSMetaDataTimeout string   `cli:"wait-for-ecs-meta-data-timeout"`
	WaitForGCPLabelsTimeout   string   `cli:"wait-for-gcp-labels-timeout"`

	GitCheckoutFlags      string `cli:"git-checkout-flags"`
	GitCloneFlags         string `cli:"git-clone-flags"`
	GitCloneMirrorFlags   string `cli:"git-clone-mirror-flags"`
	GitCleanFlags         string `cli:"git-clean-flags"`
	GitFetchFlags         string `cli:"git-fetch-flags"`
	GitMirrorsPath        string `cli:"git-mirrors-path" normalize:"filepath"`
	GitMirrorsLockTimeout int    `cli:"git-mirrors-lock-timeout"`
	GitMirrorsSkipUpdate  bool   `cli:"git-mirrors-skip-update"`
	NoGitSubmodules       bool   `cli:"no-git-submodules"`

	NoSSHKeyscan        bool     `cli:"no-ssh-keyscan"`
	NoCommandEval       bool     `cli:"no-command-eval"`
	NoLocalHooks        bool     `cli:"no-local-hooks"`
	NoPlugins           bool     `cli:"no-plugins"`
	NoPluginValidation  bool     `cli:"no-plugin-validation"`
	NoFeatureReporting  bool     `cli:"no-feature-reporting"`
	AllowedRepositories []string `cli:"allowed-repositories" normalize:"list"`
	AllowedPlugins      []string `cli:"allowed-plugins" normalize:"list"`

	EnableEnvironmentVariableAllowList bool     `cli:"enable-environment-variable-allowlist"`
	AllowedEnvironmentVariables        []string `cli:"allowed-environment-variables" normalize:"list"`

	HealthCheckAddr string `cli:"health-check-addr"`

	MetricsDatadog              bool   `cli:"metrics-datadog"`
	MetricsDatadogHost          string `cli:"metrics-datadog-host"`
	MetricsDatadogDistributions bool   `cli:"metrics-datadog-distributions"`
	TracingBackend              string `cli:"tracing-backend"`
	TracingServiceName          string `cli:"tracing-service-name"`

	// Global flags
	Debug             bool     `cli:"debug"`
	LogLevel          string   `cli:"log-level"`
	NoColor           bool     `cli:"no-color"`
	Experiments       []string `cli:"experiment" normalize:"list"`
	Profile           string   `cli:"profile"`
	StrictSingleHooks bool     `cli:"strict-single-hooks"`
	KubernetesExec    bool     `cli:"kubernetes-exec"`

	// API config
	DebugHTTP bool   `cli:"debug-http"`
	Token     string `cli:"token" validate:"required"`
	Endpoint  string `cli:"endpoint" validate:"required"`
	NoHTTP2   bool   `cli:"no-http2"`

	// Deprecated
	NoSSHFingerprintVerification bool     `cli:"no-automatic-ssh-fingerprint-verification" deprecated-and-renamed-to:"NoSSHKeyscan"`
	MetaData                     []string `cli:"meta-data" deprecated-and-renamed-to:"Tags"`
	MetaDataEC2                  bool     `cli:"meta-data-ec2" deprecated-and-renamed-to:"TagsFromEC2"`
	MetaDataEC2Tags              bool     `cli:"meta-data-ec2-tags" deprecated-and-renamed-to:"TagsFromEC2Tags"`
	MetaDataGCP                  bool     `cli:"meta-data-gcp" deprecated-and-renamed-to:"TagsFromGCP"`
	TagsFromEC2                  bool     `cli:"tags-from-ec2" deprecated-and-renamed-to:"TagsFromEC2MetaData"`
	TagsFromGCP                  bool     `cli:"tags-from-gcp" deprecated-and-renamed-to:"TagsFromGCPMetaData"`
	DisconnectAfterJobTimeout    int      `cli:"disconnect-after-job-timeout" deprecated:"Use disconnect-after-idle-timeout instead"`
}

func (asc AgentStartConfig) Features(ctx context.Context) []string {
	if asc.NoFeatureReporting {
		return []string{}
	}

	features := make([]string, 0, 8)

	if asc.GitMirrorsPath != "" {
		features = append(features, "git-mirrors")
	}

	if asc.AcquireJob != "" {
		features = append(features, "acquire-job")
	}

	if asc.TracingBackend == tracetools.BackendDatadog {
		features = append(features, "datadog-tracing")
	}

	if asc.TracingBackend == tracetools.BackendOpenTelemetry {
		features = append(features, "opentelemetry-tracing")
	}

	if asc.DisconnectAfterJob {
		features = append(features, "disconnect-after-job")
	}

	if asc.DisconnectAfterIdleTimeout != 0 {
		features = append(features, "disconnect-after-idle")
	}

	if asc.NoPlugins {
		features = append(features, "no-plugins")
	}

	if asc.NoCommandEval {
		features = append(features, "no-script-eval")
	}

	if len(asc.AllowedRepositories) > 0 {
		features = append(features, "allowed-repositories")
	}

	if len(asc.AllowedPlugins) > 0 {
		features = append(features, "allowed-plugins")
	}

	for _, exp := range experiments.Enabled(ctx) {
		features = append(features, fmt.Sprintf("experiment-%s", exp))
	}

	return features
}

func DefaultShell() string {
	// https://github.com/golang/go/blob/master/src/go/build/syslist.go#L7
	switch runtime.GOOS {
	case "windows":
		return `C:\Windows\System32\CMD.exe /S /C`
	case "freebsd", "openbsd":
		return "/usr/local/bin/bash -e -c"
	case "netbsd":
		return "/usr/pkg/bin/bash -e -c"
	default:
		return "/bin/bash -e -c"
	}
}

func defaultConfigFilePaths() (paths []string) {
	// Toggle between windows and *nix paths
	if runtime.GOOS == "windows" {
		paths = []string{
			"C:\\buildkite-agent\\buildkite-agent.cfg",
			"$USERPROFILE\\AppData\\Local\\buildkite-agent\\buildkite-agent.cfg",
			"$USERPROFILE\\AppData\\Local\\BuildkiteAgent\\buildkite-agent.cfg",
		}
	} else {
		paths = []string{
			"$HOME/.buildkite-agent/buildkite-agent.cfg",
		}

		// For Apple Silicon Macs, prioritise the `/opt/homebrew` path over `/usr/local`
		if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
			paths = append(paths, "/opt/homebrew/etc/buildkite-agent/buildkite-agent.cfg")
		}

		paths = append(paths, "/usr/local/etc/buildkite-agent/buildkite-agent.cfg", "/etc/buildkite-agent/buildkite-agent.cfg")
	}

	// Also check to see if there's a buildkite-agent.cfg in the folder
	// that the binary is running in.
	exePath, err := os.Executable()
	if err == nil {
		pathToBinary, err := filepath.Abs(filepath.Dir(exePath))
		if err == nil {
			pathToRelativeConfig := filepath.Join(pathToBinary, "buildkite-agent.cfg")
			paths = append([]string{pathToRelativeConfig}, paths...)
		}
	}

	return paths
}

var AgentStartCommand = cli.Command{
	Name:        "start",
	Usage:       "Starts a Buildkite agent",
	Description: startDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "config",
			Value:  "",
			Usage:  "Path to a configuration file",
			EnvVar: "BUILDKITE_AGENT_CONFIG",
		},
		cli.StringFlag{
			Name:   "name",
			Value:  "",
			Usage:  "The name of the agent",
			EnvVar: "BUILDKITE_AGENT_NAME",
		},
		cli.StringFlag{
			Name:   "priority",
			Value:  "",
			Usage:  "The priority of the agent (higher priorities are assigned work first)",
			EnvVar: "BUILDKITE_AGENT_PRIORITY",
		},
		cli.StringFlag{
			Name:   "acquire-job",
			Value:  "",
			Usage:  "Start this agent and only run the specified job, disconnecting after it's finished",
			EnvVar: "BUILDKITE_AGENT_ACQUIRE_JOB",
		},
		cli.BoolFlag{
			Name:   "disconnect-after-job",
			Usage:  "Disconnect the agent after running exactly one job. When used in conjunction with the ′--spawn′ flag, each worker booted will run exactly one job",
			EnvVar: "BUILDKITE_AGENT_DISCONNECT_AFTER_JOB",
		},
		cli.IntFlag{
			Name:   "disconnect-after-idle-timeout",
			Value:  0,
			Usage:  "The maximum idle time in seconds to wait for a job before disconnecting. The default of 0 means no timeout",
			EnvVar: "BUILDKITE_AGENT_DISCONNECT_AFTER_IDLE_TIMEOUT",
		},
		cancelGracePeriodFlag,
		cli.BoolFlag{
			Name:   "enable-job-log-tmpfile",
			Usage:  "Store the job logs in a temporary file ′BUILDKITE_JOB_LOG_TMPFILE′ that is accessible during the job and removed at the end of the job",
			EnvVar: "BUILDKITE_ENABLE_JOB_LOG_TMPFILE",
		},
		cli.StringFlag{
			Name:   "job-log-path",
			Usage:  "Location to store job logs created by configuring ′enable-job-log-tmpfile`, by default job log will be stored in TempDir",
			EnvVar: "BUILDKITE_JOB_LOG_PATH",
		},
		cli.BoolFlag{
			Name:   "write-job-logs-to-stdout",
			Usage:  "Writes job logs to the agent process' stdout. This simplifies log collection if running agents in Docker.",
			EnvVar: "BUILDKITE_WRITE_JOB_LOGS_TO_STDOUT",
		},
		cli.StringFlag{
			Name:   "shell",
			Value:  DefaultShell(),
			Usage:  "The shell command used to interpret build commands, e.g /bin/bash -e -c",
			EnvVar: "BUILDKITE_SHELL",
		},
		cli.StringFlag{
			Name:   "queue",
			Usage:  "The queue the agent will listen to for jobs. If not set, the agent will use the default queue. Overwrites the queue tag in the agent's tags",
			EnvVar: "BUILDKITE_AGENT_QUEUE",
		},
		cli.StringSliceFlag{
			Name:   "tags",
			Value:  &cli.StringSlice{},
			Usage:  "A comma-separated list of tags for the agent (for example, \"linux\" or \"mac,xcode=8\")",
			EnvVar: "BUILDKITE_AGENT_TAGS",
		},
		cli.BoolFlag{
			Name:   "tags-from-host",
			Usage:  "Include tags from the host (hostname, machine-id, os)",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_HOST",
		},
		cli.StringSliceFlag{
			Name:   "tags-from-ec2-meta-data",
			Value:  &cli.StringSlice{},
			Usage:  "Include the default set of host EC2 meta-data as tags (instance-id, instance-type, ami-id, and instance-life-cycle)",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_EC2_META_DATA",
		},
		cli.StringSliceFlag{
			Name:   "tags-from-ec2-meta-data-paths",
			Value:  &cli.StringSlice{},
			Usage:  "Include additional tags fetched from EC2 meta-data using tag & path suffix pairs, e.g \"tag_name=path/to/value\"",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_EC2_META_DATA_PATHS",
		},
		cli.BoolFlag{
			Name:   "tags-from-ec2-tags",
			Usage:  "Include the host's EC2 tags as tags",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_EC2_TAGS",
		},
		cli.BoolFlag{
			Name:   "tags-from-ecs-meta-data",
			Usage:  "Include the host's ECS meta-data as tags (container-name, image, and task-arn)",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_ECS_META_DATA",
		},
		cli.StringSliceFlag{
			Name:   "tags-from-gcp-meta-data",
			Value:  &cli.StringSlice{},
			Usage:  "Include the default set of host Google Cloud instance meta-data as tags (instance-id, machine-type, preemptible, project-id, region, and zone)",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_GCP_META_DATA",
		},
		cli.StringSliceFlag{
			Name:   "tags-from-gcp-meta-data-paths",
			Value:  &cli.StringSlice{},
			Usage:  "Include additional tags fetched from Google Cloud instance meta-data using tag & path suffix pairs, e.g \"tag_name=path/to/value\"",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_GCP_META_DATA_PATHS",
		},
		cli.BoolFlag{
			Name:   "tags-from-gcp-labels",
			Usage:  "Include the host's Google Cloud instance labels as tags",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_GCP_LABELS",
		},
		cli.DurationFlag{
			Name:   "wait-for-ec2-tags-timeout",
			Usage:  "The amount of time to wait for tags from EC2 before proceeding",
			EnvVar: "BUILDKITE_AGENT_WAIT_FOR_EC2_TAGS_TIMEOUT",
			Value:  time.Second * 10,
		},
		cli.DurationFlag{
			Name:   "wait-for-ec2-meta-data-timeout",
			Usage:  "The amount of time to wait for meta-data from EC2 before proceeding",
			EnvVar: "BUILDKITE_AGENT_WAIT_FOR_EC2_META_DATA_TIMEOUT",
			Value:  time.Second * 10,
		},
		cli.DurationFlag{
			Name:   "wait-for-ecs-meta-data-timeout",
			Usage:  "The amount of time to wait for meta-data from ECS before proceeding",
			EnvVar: "BUILDKITE_AGENT_WAIT_FOR_ECS_META_DATA_TIMEOUT",
			Value:  time.Second * 10,
		},
		cli.DurationFlag{
			Name:   "wait-for-gcp-labels-timeout",
			Usage:  "The amount of time to wait for labels from GCP before proceeding",
			EnvVar: "BUILDKITE_AGENT_WAIT_FOR_GCP_LABELS_TIMEOUT",
			Value:  time.Second * 10,
		},
		cli.StringFlag{
			Name:   "git-checkout-flags",
			Value:  "-f",
			Usage:  "Flags to pass to \"git checkout\" command",
			EnvVar: "BUILDKITE_GIT_CHECKOUT_FLAGS",
		},
		cli.StringFlag{
			Name:   "git-clone-flags",
			Value:  "-v",
			Usage:  "Flags to pass to the \"git clone\" command",
			EnvVar: "BUILDKITE_GIT_CLONE_FLAGS",
		},
		cli.StringFlag{
			Name:   "git-clean-flags",
			Value:  "-ffxdq",
			Usage:  "Flags to pass to \"git clean\" command",
			EnvVar: "BUILDKITE_GIT_CLEAN_FLAGS",
			// -ff: delete files and directories, including untracked nested git repositories
			// -x: don't use .gitignore rules
			// -d: recurse into untracked directories
			// -q: quiet, only report errors
		},
		cli.StringFlag{
			Name:   "git-fetch-flags",
			Value:  "-v --prune --tags",
			Usage:  "Flags to pass to \"git fetch\" command",
			EnvVar: "BUILDKITE_GIT_FETCH_FLAGS",
		},
		cli.StringFlag{
			Name:   "git-clone-mirror-flags",
			Value:  "-v",
			Usage:  "Flags to pass to the \"git clone\" command when used for mirroring",
			EnvVar: "BUILDKITE_GIT_CLONE_MIRROR_FLAGS",
		},
		cli.StringFlag{
			Name:   "git-mirrors-path",
			Value:  "",
			Usage:  "Path to where mirrors of git repositories are stored",
			EnvVar: "BUILDKITE_GIT_MIRRORS_PATH",
		},
		cli.IntFlag{
			Name:   "git-mirrors-lock-timeout",
			Value:  300,
			Usage:  "Seconds to lock a git mirror during clone, should exceed your longest checkout",
			EnvVar: "BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT",
		},
		cli.BoolFlag{
			Name:   "git-mirrors-skip-update",
			Usage:  "Skip updating the Git mirror",
			EnvVar: "BUILDKITE_GIT_MIRRORS_SKIP_UPDATE",
		},
		cli.StringFlag{
			Name:   "bootstrap-script",
			Value:  "",
			Usage:  "The command that is executed for bootstrapping a job, defaults to the bootstrap sub-command of this binary",
			EnvVar: "BUILDKITE_BOOTSTRAP_SCRIPT_PATH",
		},
		cli.StringFlag{
			Name:   "build-path",
			Value:  "",
			Usage:  "Path to where the builds will run from",
			EnvVar: "BUILDKITE_BUILD_PATH",
		},
		cli.StringFlag{
			Name:   "hooks-path",
			Value:  "",
			Usage:  "Directory where the hook scripts are found",
			EnvVar: "BUILDKITE_HOOKS_PATH",
		},
		cli.StringFlag{
			Name:   "sockets-path",
			Value:  defaultSocketsPath(),
			Usage:  "Directory where the agent will place sockets",
			EnvVar: "BUILDKITE_SOCKETS_PATH",
		},
		cli.StringFlag{
			Name:   "plugins-path",
			Value:  "",
			Usage:  "Directory where the plugins are saved to",
			EnvVar: "BUILDKITE_PLUGINS_PATH",
		},
		cli.BoolFlag{
			Name:   "no-ansi-timestamps",
			Usage:  "Do not insert ANSI timestamp codes at the start of each line of job output",
			EnvVar: "BUILDKITE_NO_ANSI_TIMESTAMPS",
		},
		cli.BoolFlag{
			Name:   "timestamp-lines",
			Usage:  "Prepend timestamps on each line of job output. Has no effect unless --no-ansi-timestamps is also used",
			EnvVar: "BUILDKITE_TIMESTAMP_LINES",
		},
		cli.StringFlag{
			Name:   "health-check-addr",
			Usage:  "Start an HTTP server on this addr:port that returns whether the agent is healthy, disabled by default",
			EnvVar: "BUILDKITE_AGENT_HEALTH_CHECK_ADDR",
		},
		cli.BoolFlag{
			Name:   "no-pty",
			Usage:  "Do not run jobs within a pseudo terminal",
			EnvVar: "BUILDKITE_NO_PTY",
		},
		cli.BoolFlag{
			Name:   "no-ssh-keyscan",
			Usage:  "Don't automatically run ssh-keyscan before checkout",
			EnvVar: "BUILDKITE_NO_SSH_KEYSCAN",
		},
		cli.BoolFlag{
			Name:   "no-command-eval",
			Usage:  "Don't allow this agent to run arbitrary console commands, including plugins",
			EnvVar: "BUILDKITE_NO_COMMAND_EVAL",
		},
		cli.BoolFlag{
			Name:   "no-plugins",
			Usage:  "Don't allow this agent to load plugins",
			EnvVar: "BUILDKITE_NO_PLUGINS",
		},
		cli.BoolTFlag{
			Name:   "no-plugin-validation",
			Usage:  "Don't validate plugin configuration and requirements",
			EnvVar: "BUILDKITE_NO_PLUGIN_VALIDATION",
		},
		cli.BoolFlag{
			Name:   "no-local-hooks",
			Usage:  "Don't allow local hooks to be run from checked out repositories",
			EnvVar: "BUILDKITE_NO_LOCAL_HOOKS",
		},
		cli.BoolFlag{
			Name:   "no-git-submodules",
			Usage:  "Don't automatically checkout git submodules",
			EnvVar: "BUILDKITE_NO_GIT_SUBMODULES,BUILDKITE_DISABLE_GIT_SUBMODULES",
		},
		cli.BoolFlag{
			Name:   "no-feature-reporting",
			Usage:  "Disables sending a list of enabled features back to the Buildkite mothership. We use this information to measure feature usage, but if you're not comfortable sharing that information then that's totally okay :)",
			EnvVar: "BUILDKITE_AGENT_NO_FEATURE_REPORTING",
		},
		cli.StringSliceFlag{
			Name:   "allowed-repositories",
			Value:  &cli.StringSlice{},
			Usage:  `A comma-separated list of regular expressions representing repositories the agent is allowed to clone (for example, "^git@github.com:buildkite/.*" or "^https://github.com/buildkite/.*")`,
			EnvVar: "BUILDKITE_ALLOWED_REPOSITORIES",
		},
		cli.BoolFlag{
			Name:   "enable-environment-variable-allowlist",
			Usage:  "Only run jobs where all environment variables are allowed by the allowed-environment-variables option, or have been set by Buildkite",
			EnvVar: "BUILDKITE_ENABLE_ENVIRONMENT_VARIABLE_ALLOWLIST",
		},
		cli.StringSliceFlag{
			Name:   "allowed-environment-variables",
			Value:  &cli.StringSlice{},
			Usage:  `A comma-separated list of regular expressions representing environment variables the agent will pass to jobs (for example, "^MYAPP_.*$"). Environment variables set by Buildkite will always be allowed. Requires --enable-environment-variable-allowlist to be set`,
			EnvVar: "BUILDKITE_ALLOWED_ENVIRONMENT_VARIABLES",
		},
		cli.StringSliceFlag{
			Name:   "allowed-plugins",
			Value:  &cli.StringSlice{},
			Usage:  `A comma-separated list of regular expressions representing plugins the agent is allowed to use (for example, "^buildkite-plugins/.*$" or "^/var/lib/buildkite-plugins/.*")`,
			EnvVar: "BUILDKITE_ALLOWED_PLUGINS",
		},
		cli.BoolFlag{
			Name:   "metrics-datadog",
			Usage:  "Send metrics to DogStatsD for Datadog",
			EnvVar: "BUILDKITE_METRICS_DATADOG",
		},
		cli.StringFlag{
			Name:   "metrics-datadog-host",
			Usage:  "The dogstatsd instance to send metrics to using udp",
			EnvVar: "BUILDKITE_METRICS_DATADOG_HOST",
			Value:  "127.0.0.1:8125",
		},
		cli.BoolFlag{
			Name:   "metrics-datadog-distributions",
			Usage:  "Use Datadog Distributions for Timing metrics",
			EnvVar: "BUILDKITE_METRICS_DATADOG_DISTRIBUTIONS",
		},
		cli.StringFlag{
			Name:   "log-format",
			Usage:  "The format to use for the logger output",
			EnvVar: "BUILDKITE_LOG_FORMAT",
			Value:  "text",
		},
		cli.IntFlag{
			Name:   "spawn",
			Usage:  "The number of agents to spawn in parallel (mutually exclusive with --spawn-per-cpu)",
			Value:  1,
			EnvVar: "BUILDKITE_AGENT_SPAWN",
		},
		cli.IntFlag{
			Name:   "spawn-per-cpu",
			Usage:  "The number of agents to spawn per cpu in parallel (mutually exclusive with --spawn)",
			Value:  0,
			EnvVar: "BUILDKITE_AGENT_SPAWN_PER_CPU",
		},
		cli.BoolFlag{
			Name:   "spawn-with-priority",
			Usage:  "Assign priorities to every spawned agent (when using --spawn or --spawn-per-cpu) equal to the agent's index",
			EnvVar: "BUILDKITE_AGENT_SPAWN_WITH_PRIORITY",
		},
		cancelSignalFlag,
		signalGracePeriodSecondsFlag,
		cli.StringFlag{
			Name:   "tracing-backend",
			Usage:  `Enable tracing for build jobs by specifying a backend, "datadog" or "opentelemetry"`,
			EnvVar: "BUILDKITE_TRACING_BACKEND",
			Value:  "",
		},
		cli.StringFlag{
			Name:   "tracing-service-name",
			Usage:  "Service name to use when reporting traces.",
			EnvVar: "BUILDKITE_TRACING_SERVICE_NAME",
			Value:  "buildkite-agent",
		},
		cli.StringFlag{
			Name:   "verification-jwks-file",
			Usage:  "Path to a file containing a JSON Web Key Set (JWKS), used to verify job signatures. ",
			EnvVar: "BUILDKITE_AGENT_VERIFICATION_JWKS_FILE",
		},
		cli.StringFlag{
			Name:   "signing-jwks-file",
			Usage:  "Path to a file containing a signing key. Passing this flag enables pipeline signing for all pipelines uploaded by this agent. For hmac-sha256, the raw file content is used as the shared key",
			EnvVar: "BUILDKITE_AGENT_SIGNING_JWKS_FILE",
		},
		cli.StringFlag{
			Name:   "signing-jwks-key-id",
			Usage:  "The JWKS key ID to use when signing the pipeline. If omitted, and the signing JWKS contains only one key, that key will be used.",
			EnvVar: "BUILDKITE_AGENT_SIGNING_JWKS_KEY_ID",
		},
		cli.BoolFlag{
			Name:   "debug-signing",
			Usage:  "Enable debug logging for pipeline signing. This can potentially leak secrets to the logs as it prints each step in full before signing. Requires debug logging to be enabled",
			EnvVar: "BUILDKITE_AGENT_DEBUG_SIGNING",
		},
		cli.StringFlag{
			Name:   "verification-failure-behavior",
			Value:  agent.VerificationBehaviourBlock,
			Usage:  fmt.Sprintf("The behavior when a job is received without a signature. One of: %v. Defaults to %s", verificationFailureBehaviors, agent.VerificationBehaviourBlock),
			EnvVar: "BUILDKITE_AGENT_JOB_VERIFICATION_NO_SIGNATURE_BEHAVIOR",
		},
		cli.StringSliceFlag{
			Name:   "disable-warnings-for",
			Usage:  "A list of warning IDs to disable",
			EnvVar: "BUILDKITE_AGENT_DISABLE_WARNINGS_FOR",
		},

		// API Flags
		AgentRegisterTokenFlag,
		EndpointFlag,
		NoHTTP2Flag,
		DebugHTTPFlag,

		// Global flags
		NoColorFlag,
		DebugFlag,
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
		RedactedVars,
		StrictSingleHooksFlag,
		KubernetesExecFlag,

		// Deprecated flags which will be removed in v4
		cli.StringSliceFlag{
			Name:   "meta-data",
			Value:  &cli.StringSlice{},
			Hidden: true,
			EnvVar: "BUILDKITE_AGENT_META_DATA",
		},
		cli.BoolFlag{
			Name:   "meta-data-ec2",
			Hidden: true,
			EnvVar: "BUILDKITE_AGENT_META_DATA_EC2",
		},
		cli.BoolFlag{
			Name:   "meta-data-ec2-tags",
			Hidden: true,
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_EC2_TAGS",
		},
		cli.BoolFlag{
			Name:   "meta-data-gcp",
			Hidden: true,
			EnvVar: "BUILDKITE_AGENT_META_DATA_GCP",
		},
		cli.BoolFlag{
			Name:   "no-automatic-ssh-fingerprint-verification",
			Hidden: true,
			EnvVar: "BUILDKITE_NO_AUTOMATIC_SSH_FINGERPRINT_VERIFICATION",
		},
		cli.BoolFlag{
			Name:   "tags-from-ec2",
			Usage:  "Include the host's EC2 meta-data as tags (instance-id, instance-type, and ami-id)",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_EC2",
		},
		cli.BoolFlag{
			Name:   "tags-from-gcp",
			Usage:  "Include the host's Google Cloud instance meta-data as tags (instance-id, machine-type, preemptible, project-id, region, and zone)",
			EnvVar: "BUILDKITE_AGENT_TAGS_FROM_GCP",
		},
		cli.IntFlag{
			Name:   "disconnect-after-job-timeout",
			Hidden: true,
			Usage:  "When --disconnect-after-job is specified, the number of seconds to wait for a job before shutting down",
			EnvVar: "BUILDKITE_AGENT_DISCONNECT_AFTER_JOB_TIMEOUT",
		},
	},
	Action: func(c *cli.Context) error {
		ctx := context.Background()
		ctx, cfg, l, configFile, done := setupLoggerAndConfig[AgentStartConfig](ctx, c, withConfigFilePaths(
			defaultConfigFilePaths(),
		))
		defer done()

		// Remove any config env from the environment to prevent them propagating to bootstrap
		if err := UnsetConfigFromEnvironment(c); err != nil {
			return fmt.Errorf("failed to unset config from environment: %w", err)
		}

		if cfg.VerificationJWKSFile != "" {
			if !slices.Contains(verificationFailureBehaviors, cfg.VerificationFailureBehavior) {
				return fmt.Errorf(
					"invalid job verification no signature behavior %q. Must be one of: %v",
					cfg.VerificationFailureBehavior,
					verificationFailureBehaviors,
				)
			}
		}

		// Force some settings if on Windows (these aren't supported yet)
		if runtime.GOOS == "windows" {
			cfg.NoPTY = true
		}

		// Set a useful default for the bootstrap script
		if cfg.BootstrapScript == "" {
			exePath, err := os.Executable()
			if err != nil {
				return errors.New("unable to find executable path for bootstrap")
			}
			cfg.BootstrapScript = fmt.Sprintf("%s bootstrap", shellwords.Quote(exePath))
		}

		isSetNoPlugins := c.IsSet("no-plugins")
		if configFile != nil {
			if _, exists := configFile.Config["no-plugins"]; exists {
				isSetNoPlugins = true
			}
		}

		// Show a warning if plugins are enabled by no-command-eval or no-local-hooks is set
		if isSetNoPlugins && !cfg.NoPlugins {
			msg := "Plugins have been specifically enabled, despite %s being enabled. " +
				"Plugins can execute arbitrary hooks and commands, make sure you are " +
				"whitelisting your plugins in " +
				"your environment hook."

			switch {
			case cfg.NoCommandEval:
				l.Warn(msg, "no-command-eval")
			case cfg.NoLocalHooks:
				l.Warn(msg, "no-local-hooks")
			}
		}

		// Turning off command eval or local hooks will also turn off plugins unless
		// `--no-plugins=false` is provided specifically
		if (cfg.NoCommandEval || cfg.NoLocalHooks) && !isSetNoPlugins {
			cfg.NoPlugins = true
		}

		// Guess the shell if none is provided
		if cfg.Shell == "" {
			cfg.Shell = DefaultShell()
		}

		// Handle deprecated DisconnectAfterJobTimeout
		if cfg.DisconnectAfterJobTimeout > 0 {
			cfg.DisconnectAfterIdleTimeout = cfg.DisconnectAfterJobTimeout
		}

		var ec2TagTimeout time.Duration
		if t := cfg.WaitForEC2TagsTimeout; t != "" {
			var err error
			ec2TagTimeout, err = time.ParseDuration(t)
			if err != nil {
				return fmt.Errorf("failed to parse ec2 tag timeout: %w", err)
			}
		}

		var ec2MetaDataTimeout time.Duration
		if t := cfg.WaitForEC2MetaDataTimeout; t != "" {
			var err error
			ec2MetaDataTimeout, err = time.ParseDuration(t)
			if err != nil {
				return fmt.Errorf("failed to parse ec2 meta-data timeout: %w", err)
			}
		}

		var ecsMetaDataTimeout time.Duration
		if t := cfg.WaitForECSMetaDataTimeout; t != "" {
			var err error
			ecsMetaDataTimeout, err = time.ParseDuration(t)
			if err != nil {
				return fmt.Errorf("failed to parse ecs meta-data timeout: %w", err)
			}
		}

		var gcpLabelsTimeout time.Duration
		if t := cfg.WaitForGCPLabelsTimeout; t != "" {
			var err error
			gcpLabelsTimeout, err = time.ParseDuration(t)
			if err != nil {
				return fmt.Errorf("failed to parse gcp labels timeout: %w", err)
			}
		}

		// Treat a negative signal grace period as relative to the cancel grace period
		if cfg.SignalGracePeriodSeconds < 0 {
			if cfg.CancelGracePeriod < -cfg.SignalGracePeriodSeconds {
				return fmt.Errorf(
					"cancel-grace-period (%d) must be at least as big as signal-grace-period-seconds (%d)",
					cfg.CancelGracePeriod,
					cfg.SignalGracePeriodSeconds,
				)
			}
			cfg.SignalGracePeriodSeconds = cfg.CancelGracePeriod + cfg.SignalGracePeriodSeconds
		}

		if cfg.CancelGracePeriod <= cfg.SignalGracePeriodSeconds {
			return fmt.Errorf(
				"cancel-grace-period (%d) must be greater than signal-grace-period-seconds (%d)",
				cfg.CancelGracePeriod,
				cfg.SignalGracePeriodSeconds,
			)
		}

		signalGracePeriod := time.Duration(cfg.SignalGracePeriodSeconds) * time.Second

		mc := metrics.NewCollector(l, metrics.CollectorConfig{
			Datadog:              cfg.MetricsDatadog,
			DatadogHost:          cfg.MetricsDatadogHost,
			DatadogDistributions: cfg.MetricsDatadogDistributions,
		})

		// Sense check supported tracing backends, we don't want bootstrapped jobs to silently have no tracing
		if _, has := tracetools.ValidTracingBackends[cfg.TracingBackend]; !has {
			return fmt.Errorf(
				"the given tracing backend %q is not supported. Valid backends are: %q",
				cfg.TracingBackend,
				maps.Keys(tracetools.ValidTracingBackends),
			)
		}

		if experiments.IsEnabled(ctx, experiments.AgentAPI) {
			shutdown, err := runAgentAPI(ctx, l, cfg.SocketsPath)
			if err != nil {
				return err
			}
			defer shutdown()
		}

		var verificationJWKS jwk.Set
		if cfg.VerificationJWKSFile != "" {
			var err error
			verificationJWKS, err = parseAndValidateJWKS(ctx, "verification", cfg.VerificationJWKSFile)
			if err != nil {
				l.Fatal("Verification JWKS failed validation: %v", err)
			}
		}

		if cfg.SigningJWKSFile != "" {
			// The actual JWKS itself doesn't get used until `buildkite-agent pipeline upload` is called, but validate it here anyway
			_, err := parseAndValidateJWKS(ctx, "signing", cfg.SigningJWKSFile)
			if err != nil {
				l.Fatal("Signing JWKS failed validation: %v", err)
			}
		}

		if len(cfg.AllowedEnvironmentVariables) > 0 && !cfg.EnableEnvironmentVariableAllowList {
			l.Fatal("allowed-environment-variables is set, but enable-environment-variable-allowlist is not set")
		}

		var allowedEnvironmentVariables []*regexp.Regexp
		if cfg.EnableEnvironmentVariableAllowList {
			allowedEnvironmentVariables = append(allowedEnvironmentVariables, buildkiteSetEnvironmentVariables...)

			for _, v := range cfg.AllowedEnvironmentVariables {
				re, err := regexp.Compile(v)
				if err != nil {
					l.Fatal("Regex %s in allowed-environment-variables failed to compile: %v", v, err)
				}

				allowedEnvironmentVariables = append(allowedEnvironmentVariables, re)
			}
		}

		// AgentConfiguration is the runtime configuration for an agent
		agentConf := agent.AgentConfiguration{
			BootstrapScript:              cfg.BootstrapScript,
			BuildPath:                    cfg.BuildPath,
			SocketsPath:                  cfg.SocketsPath,
			GitMirrorsPath:               cfg.GitMirrorsPath,
			GitMirrorsLockTimeout:        cfg.GitMirrorsLockTimeout,
			GitMirrorsSkipUpdate:         cfg.GitMirrorsSkipUpdate,
			HooksPath:                    cfg.HooksPath,
			PluginsPath:                  cfg.PluginsPath,
			GitCheckoutFlags:             cfg.GitCheckoutFlags,
			GitCloneFlags:                cfg.GitCloneFlags,
			GitCloneMirrorFlags:          cfg.GitCloneMirrorFlags,
			GitCleanFlags:                cfg.GitCleanFlags,
			GitFetchFlags:                cfg.GitFetchFlags,
			GitSubmodules:                !cfg.NoGitSubmodules,
			SSHKeyscan:                   !cfg.NoSSHKeyscan,
			CommandEval:                  !cfg.NoCommandEval,
			PluginsEnabled:               !cfg.NoPlugins,
			PluginValidation:             !cfg.NoPluginValidation,
			LocalHooksEnabled:            !cfg.NoLocalHooks,
			AllowedEnvironmentVariables:  allowedEnvironmentVariables,
			StrictSingleHooks:            cfg.StrictSingleHooks,
			RunInPty:                     !cfg.NoPTY,
			ANSITimestamps:               !cfg.NoANSITimestamps,
			TimestampLines:               cfg.TimestampLines,
			DisconnectAfterJob:           cfg.DisconnectAfterJob,
			DisconnectAfterIdleTimeout:   cfg.DisconnectAfterIdleTimeout,
			CancelGracePeriod:            cfg.CancelGracePeriod,
			SignalGracePeriod:            signalGracePeriod,
			EnableJobLogTmpfile:          cfg.EnableJobLogTmpfile,
			JobLogPath:                   cfg.JobLogPath,
			WriteJobLogsToStdout:         cfg.WriteJobLogsToStdout,
			LogFormat:                    cfg.LogFormat,
			Shell:                        cfg.Shell,
			RedactedVars:                 cfg.RedactedVars,
			AcquireJob:                   cfg.AcquireJob,
			TracingBackend:               cfg.TracingBackend,
			TracingServiceName:           cfg.TracingServiceName,
			VerificationFailureBehaviour: cfg.VerificationFailureBehavior,
			KubernetesExec:               cfg.KubernetesExec,

			SigningJWKSFile:  cfg.SigningJWKSFile,
			SigningJWKSKeyID: cfg.SigningJWKSKeyID,
			DebugSigning:     cfg.DebugSigning,

			VerificationJWKS: verificationJWKS,

			DisableWarningsFor: cfg.DisableWarningsFor,
		}

		if configFile != nil {
			agentConf.ConfigPath = configFile.Path
		}

		if cfg.LogFormat == "text" {
			welcomeMessage :=
				"\n" +
					"%s   _           _ _     _ _    _ _                                _\n" +
					"  | |         (_) |   | | |  (_) |                              | |\n" +
					"  | |__  _   _ _| | __| | | ___| |_ ___    __ _  __ _  ___ _ __ | |_\n" +
					"  | '_ \\| | | | | |/ _` | |/ / | __/ _ \\  / _` |/ _` |/ _ \\ '_ \\| __|\n" +
					"  | |_) | |_| | | | (_| |   <| | ||  __/ | (_| | (_| |  __/ | | | |_\n" +
					"  |_.__/ \\__,_|_|_|\\__,_|_|\\_\\_|\\__\\___|  \\__,_|\\__, |\\___|_| |_|\\__|\n" +
					"                                                 __/ |\n" +
					" https://buildkite.com/agent                    |___/\n%s\n"

			if !cfg.NoColor {
				fmt.Fprintf(os.Stderr, welcomeMessage, "\x1b[38;5;48m", "\x1b[0m")
			} else {
				fmt.Fprintf(os.Stderr, welcomeMessage, "", "")
			}
		} else if cfg.LogFormat != "json" {
			// TODO If/when cli is upgraded to v2, choice validation can be done with per-argument Actions.
			return fmt.Errorf("invalid log format %q. Only 'text' or 'json' are allowed.", cfg.LogFormat)
		}

		l.Notice("Starting buildkite-agent v%s with PID: %s", version.Version(), fmt.Sprintf("%d", os.Getpid()))
		l.Notice("The agent source code can be found here: https://github.com/buildkite/agent")
		l.Notice("For questions and support, email us at: hello@buildkite.com")

		if agentConf.ConfigPath != "" {
			l.WithFields(logger.StringField(`path`, agentConf.ConfigPath)).Info("Configuration loaded")
		}

		l.Debug("Bootstrap command: %s", agentConf.BootstrapScript)
		l.Debug("Build path: %s", agentConf.BuildPath)
		l.Debug("Hooks directory: %s", agentConf.HooksPath)
		l.Debug("Plugins directory: %s", agentConf.PluginsPath)

		if exps := experiments.KnownAndEnabled(ctx); len(exps) > 0 {
			l.WithFields(logger.StringField("experiments", fmt.Sprintf("%v", exps))).Info("Experiments are enabled")
		}

		if !agentConf.SSHKeyscan {
			l.Info("Automatic ssh-keyscan has been disabled")
		}

		if !agentConf.CommandEval {
			l.Info("Evaluating console commands has been disabled")
		}

		if !agentConf.PluginsEnabled {
			l.Info("Plugins have been disabled")
		}

		if !agentConf.RunInPty {
			l.Info("Running builds within a pseudoterminal (PTY) has been disabled")
		}

		if agentConf.DisconnectAfterJob {
			l.Info("Agents will disconnect after a job run has completed")
		}

		if agentConf.DisconnectAfterIdleTimeout > 0 {
			l.Info("Agents will disconnect after %d seconds of inactivity", agentConf.DisconnectAfterIdleTimeout)
		}

		if len(cfg.AllowedRepositories) > 0 {
			agentConf.AllowedRepositories = make([]*regexp.Regexp, 0, len(cfg.AllowedRepositories))
			for _, v := range cfg.AllowedRepositories {
				r, err := regexp.Compile(v)
				if err != nil {
					l.Fatal("Regex %s is allowed-repositories failed to compile: %v", v, err)
				}
				agentConf.AllowedRepositories = append(agentConf.AllowedRepositories, r)
			}
			l.Info("Allowed repositories patterns: %q", agentConf.AllowedRepositories)
		}

		if len(cfg.AllowedPlugins) > 0 {
			agentConf.AllowedPlugins = make([]*regexp.Regexp, 0, len(cfg.AllowedPlugins))
			for _, v := range cfg.AllowedPlugins {
				r, err := regexp.Compile(v)
				if err != nil {
					l.Fatal("Regex %s in allowed-plugins failed to compile: %v", v, err)
				}
				agentConf.AllowedPlugins = append(agentConf.AllowedPlugins, r)
			}
			l.Info("Allowed plugins patterns: %q", agentConf.AllowedPlugins)
		}

		cancelSig, err := process.ParseSignal(cfg.CancelSignal)
		if err != nil {
			return fmt.Errorf("failed to parse cancel-signal: %w", err)
		}

		tags := agent.FetchTags(ctx, l, agent.FetchTagsConfig{
			Tags:                      cfg.Tags,
			TagsFromK8s:               cfg.KubernetesExec,
			TagsFromEC2MetaData:       (cfg.TagsFromEC2MetaData || cfg.TagsFromEC2),
			TagsFromEC2MetaDataPaths:  cfg.TagsFromEC2MetaDataPaths,
			TagsFromEC2Tags:           cfg.TagsFromEC2Tags,
			TagsFromECSMetaData:       cfg.TagsFromECSMetaData,
			TagsFromGCPMetaData:       (cfg.TagsFromGCPMetaData || cfg.TagsFromGCP),
			TagsFromGCPMetaDataPaths:  cfg.TagsFromGCPMetaDataPaths,
			TagsFromGCPLabels:         cfg.TagsFromGCPLabels,
			TagsFromHost:              cfg.TagsFromHost,
			WaitForEC2TagsTimeout:     ec2TagTimeout,
			WaitForEC2MetaDataTimeout: ec2MetaDataTimeout,
			WaitForECSMetaDataTimeout: ecsMetaDataTimeout,
			WaitForGCPLabelsTimeout:   gcpLabelsTimeout,
		})

		// Munge the value from --queue (if it exists) into the tags slice
		if cfg.Queue != "" {
			i := slices.IndexFunc(tags, func(s string) bool {
				return strings.HasPrefix(strings.TrimSpace(s), "queue=")
			})
			if i != -1 {
				l.Fatal("Queue must be present in only one of the --tags or the --queue flags")
			}
			tags = append(tags, "queue="+cfg.Queue)
		}

		// confirm the BuildPath is exists. The bootstrap is going to write to it when a job executes,
		// so we may as well check that'll work now and fail early if it's a problem
		if !utils.FileExists(agentConf.BuildPath) {
			l.Info("Build Path doesn't exist, creating it (%s)", agentConf.BuildPath)
			// Actual file permissions will be reduced by umask, and won't be 0777 unless the user has manually changed the umask to 000
			if err := os.MkdirAll(agentConf.BuildPath, 0o777); err != nil {
				return fmt.Errorf("failed to create builds path: %w", err)
			}
		}

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, "Token"))

		// The registration request for all agents
		registerReq := api.AgentRegisterRequest{
			Name:              cfg.Name,
			Priority:          cfg.Priority,
			ScriptEvalEnabled: !cfg.NoCommandEval,
			Tags:              tags,
			// We only want this agent to be ignored in Buildkite
			// dispatches if it's being booted to acquire a
			// specific job.
			IgnoreInDispatches: cfg.AcquireJob != "",
			Features:           cfg.Features(ctx),
		}

		if cfg.SpawnPerCPU > 0 {
			if cfg.Spawn > 1 {
				return errors.New("You can't specify spawn and spawn-per-cpu at the same time")
			}
			cfg.Spawn = runtime.NumCPU() * cfg.SpawnPerCPU
		}

		// Spawning multiple agents doesn't work if the agent is being
		// booted in acquisition mode
		if cfg.Spawn > 1 && cfg.AcquireJob != "" {
			return errors.New("You can't spawn multiple agents and acquire a job at the same time")
		}

		var workers []*agent.AgentWorker

		for i := 1; i <= cfg.Spawn; i++ {
			if cfg.Spawn == 1 {
				l.Info("Registering agent with Buildkite...")
			} else {
				l.Info("Registering agent %d of %d with Buildkite...", i, cfg.Spawn)
			}

			// Handle per-spawn name interpolation, replacing %spawn with the spawn index
			registerReq.Name = strings.ReplaceAll(cfg.Name, "%spawn", strconv.Itoa(i))

			if cfg.SpawnWithPriority {
				p := i
				if experiments.IsEnabled(ctx, experiments.DescendingSpawnPriority) {
					// This experiment helps jobs be assigned across all hosts
					// in cases where the value of --spawn varies between hosts.
					p = -i
				}
				l.Info("Assigning priority %d for agent %d", p, i)
				registerReq.Priority = strconv.Itoa(p)
			}

			// Register the agent with the buildkite API
			ag, err := agent.Register(ctx, l, client, registerReq)
			if err != nil {
				return err
			}

			// Create an agent worker to run the agent
			workers = append(workers, agent.NewAgentWorker(
				l.WithFields(logger.StringField("agent", ag.Name)),
				ag,
				mc,
				client,
				agent.AgentWorkerConfig{
					AgentConfiguration: agentConf,
					CancelSignal:       cancelSig,
					SignalGracePeriod:  signalGracePeriod,
					Debug:              cfg.Debug,
					DebugHTTP:          cfg.DebugHTTP,
					SpawnIndex:         i,
					AgentStdout:        os.Stdout,
				},
			))
		}

		// Setup the agent pool that spawns agent workers
		pool := agent.NewAgentPool(workers)

		// Agent-wide shutdown hook. Once per agent, for all workers on the agent.
		defer agentShutdownHook(l, cfg)

		// Once the shutdown hook has been setup, trigger the startup hook.
		if err := agentStartupHook(l, cfg); err != nil {
			return fmt.Errorf("failed to run startup hook: %w", err)
		}

		// Handle process signals
		signals := handlePoolSignals(ctx, l, pool)
		defer close(signals)

		l.Info("Starting %d Agent(s)", cfg.Spawn)
		l.Info("You can press Ctrl-C to stop the agents")

		// Determine the health check listening address and port for this agent
		if cfg.HealthCheckAddr != "" {
			pool.StartStatusServer(ctx, l, cfg.HealthCheckAddr)
		}

		err = pool.Start(ctx)
		if errors.Is(err, agent.ErrJobAcquisitionFailure) {
			// If the agent tried to acquire a job, but it couldn't because the job was already taken, we should exit with a
			// specific exit code so that the caller can know that this job can't be acquired.

			const acquisitionFailedExitCode = 27 // chosen by fair dice roll
			return cli.NewExitError(err, acquisitionFailedExitCode)
		}

		return err
	},
}

func parseAndValidateJWKS(ctx context.Context, keysetType, path string) (jwk.Set, error) {
	jwksBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Failed to read job %s keyset: %w", keysetType, err)
	}

	jwks, err := jwk.Parse(jwksBytes)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse job %s keyset: %w", keysetType, err)
	}

	if jwks.Len() == 0 {
		return nil, fmt.Errorf("Job %s keyset is empty", keysetType)
	}

	iter := jwks.Keys(ctx)
	for iter.Next(ctx) {
		keyI := iter.Pair().Value
		key, ok := keyI.(jwk.Key)
		if !ok {
			return nil, fmt.Errorf("Job %s keyset contains a non-key at index %d", keysetType, iter.Pair().Index)
		}

		if _, ok = key.Get(jwk.AlgorithmKey); !ok {
			return nil, fmt.Errorf("Job %s keyset contains a key without an algorithm at index %d. All keys used for signing and verification in the agent must have their `alg` key set", keysetType, iter.Pair().Index)
		}
	}

	return jwks, nil
}

func handlePoolSignals(ctx context.Context, l logger.Logger, pool *agent.AgentPool) chan os.Signal {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGQUIT)

	go func() {
		_, setStatus, done := status.AddSimpleItem(ctx, "Handle Pool Signals")
		defer done()
		setStatus("⏳ Waiting for a signal")

		var interruptCount int

		for sig := range signals {
			l.Debug("Received signal `%v`", sig)
			setStatus(fmt.Sprintf("Received signal `%v`", sig))

			switch sig {
			case syscall.SIGQUIT:
				l.Debug("Received signal `%s`", sig.String())
				pool.Stop(false)
			case syscall.SIGTERM, syscall.SIGINT:
				l.Debug("Received signal `%s`", sig.String())
				if interruptCount == 0 {
					interruptCount++
					l.Info("Received CTRL-C, send again to forcefully kill the agent(s)")
					pool.Stop(true)
				} else {
					l.Info("Forcefully stopping running jobs and stopping the agent(s)")
					pool.Stop(false)
				}
			default:
				l.Debug("Ignoring signal `%s`", sig.String())
			}
		}
	}()

	return signals
}

func agentStartupHook(log logger.Logger, cfg AgentStartConfig) error {
	return agentLifecycleHook("agent-startup", log, cfg)
}

func agentShutdownHook(log logger.Logger, cfg AgentStartConfig) {
	_ = agentLifecycleHook("agent-shutdown", log, cfg)
}

// agentLifecycleHook looks for a hook script in the hooks path
// and executes it if found. Output (stdout + stderr) is streamed into the main
// agent logger. Exit status failure is logged and returned for the caller to handle
func agentLifecycleHook(hookName string, log logger.Logger, cfg AgentStartConfig) error {
	// search for hook (including .bat & .ps1 files on Windows)
	p, err := hook.Find(cfg.HooksPath, hookName)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Error("Error finding %q hook: %v", hookName, err)
			return err
		}
		return nil
	}
	sh, err := shell.New()
	if err != nil {
		log.Error("creating shell for %q hook: %v", hookName, err)
		return err
	}

	// pipe from hook output to logger
	r, w := io.Pipe()
	sh.Logger = shell.NewWriterLogger(w, !cfg.NoColor, nil) // for Promptf
	sh.Writer = w                                           // for stdout+stderr
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		scan := bufio.NewScanner(r) // log each line separately
		log = log.WithFields(logger.StringField("hook", hookName))
		for scan.Scan() {
			log.Info(scan.Text())
		}
	}()

	// run hook
	sh.Promptf("%s", p)
	if err = sh.RunScript(context.Background(), p, nil); err != nil {
		log.Error("%q hook: %v", hookName, err)
		return err
	}
	w.Close() // goroutine scans until pipe is closed

	// wait for hook to finish and output to flush to logger
	wg.Wait()
	return nil
}

func defaultSocketsPath() string {
	home, err := homedir.Dir()
	if err != nil {
		return filepath.Join(os.TempDir(), "buildkite-sockets")
	}

	return filepath.Join(home, ".buildkite-agent", "sockets")
}

// runAgentAPI runs an API socket that can be used to interact with this
// (top-level) agent. It returns a shutdown function.
func runAgentAPI(ctx context.Context, l logger.Logger, socketsPath string) (func(), error) {
	path := agentapi.DefaultSocketPath(socketsPath)
	// There should be only one Agent API socket per agent process.
	// If a previous agent crashed and left behind a socket, we can
	// remove it.
	os.Remove(path)

	svr, err := agentapi.NewServer(path, l)
	if err != nil {
		return nil, fmt.Errorf("couldn't create Agent API server: %w", err)
	}

	if err := svr.Start(); err != nil {
		return nil, fmt.Errorf("couldn't start Agent API server: %w", err)
	}

	// Try to be the leader - no worries if this fails.
	leaderPath := agentapi.LeaderPath(socketsPath)
	if err := os.Symlink(path, leaderPath); err == nil {
		l.Info("Agent API: This agent became leader")
	}

	// Whoever the leader is, ping them every so often as a health-check.
	go leaderPinger(ctx, l, path, leaderPath)

	return func() {
		svr.Shutdown(ctx)
		if d, err := os.Readlink(leaderPath); err == nil && d == path {
			os.Remove(leaderPath)
		}
	}, nil
}

// leaderPinger pings the leader socket for liveness, and takes over if it
// fails.
func leaderPinger(ctx context.Context, l logger.Logger, path, leaderPath string) {
	pingLeader := func() error {
		d, err := os.Readlink(leaderPath)
		if err != nil {
			// Not a symlink?
			return err
		}
		if d == path {
			// It's me! Don't bother pinging.
			return nil
		}

		ctx, canc := context.WithTimeout(ctx, 100*time.Millisecond)
		defer canc()

		cl, err := agentapi.NewClient(ctx, leaderPath)
		if err != nil {
			return err
		}
		return cl.Ping(ctx)
	}

	for range time.Tick(100 * time.Millisecond) {
		if err := pingLeader(); err != nil {
			l.Warn("Agent API: Leader ping failed, staging coup: %v", err)
			l.Warn("Agent API: Leader state (locks) has been lost!")
			os.Remove(leaderPath)
			os.Symlink(path, leaderPath)
		}
	}
}
