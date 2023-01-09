package clicommand

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/bootstrap/shell"
	"github.com/buildkite/agent/v3/experiments"
	"github.com/buildkite/agent/v3/hook"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/metrics"
	"github.com/buildkite/agent/v3/process"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/agent/v3/utils"
	"github.com/buildkite/shellwords"
	"github.com/urfave/cli"
	"golang.org/x/exp/maps"
)

var StartDescription = `Usage:

   buildkite-agent start [options...]

Description:

   When a job is ready to run it will call the "bootstrap-script"
   and pass it all the environment variables required for the job to run.
   This script is responsible for checking out the code, and running the
   actual build script defined in the pipeline.

   The agent will run any jobs within a PTY (pseudo terminal) if available.

Example:

   $ buildkite-agent start --token xxx`

// Adding config requires changes in a few different spots
// - The AgentStartConfig struct with a cli parameter
// - As a flag in the AgentStartCommand (with matching env)
// - Into an env to be passed to the bootstrap in agent/job_runner.go, createEnvironment()
// - Into clicommand/bootstrap.go to read it from the env into the bootstrap config

type AgentStartConfig struct {
	Config                      string   `cli:"config"`
	Name                        string   `cli:"name"`
	Priority                    string   `cli:"priority"`
	AcquireJob                  string   `cli:"acquire-job"`
	DisconnectAfterJob          bool     `cli:"disconnect-after-job"`
	DisconnectAfterIdleTimeout  int      `cli:"disconnect-after-idle-timeout"`
	BootstrapScript             string   `cli:"bootstrap-script" normalize:"commandpath"`
	CancelGracePeriod           int      `cli:"cancel-grace-period"`
	EnableJobLogTmpfile         bool     `cli:"enable-job-log-tmpfile"`
	BuildPath                   string   `cli:"build-path" normalize:"filepath" validate:"required"`
	HooksPath                   string   `cli:"hooks-path" normalize:"filepath"`
	PluginsPath                 string   `cli:"plugins-path" normalize:"filepath"`
	Shell                       string   `cli:"shell"`
	Tags                        []string `cli:"tags" normalize:"list"`
	TagsFromEC2MetaData         bool     `cli:"tags-from-ec2-meta-data"`
	TagsFromEC2MetaDataPaths    []string `cli:"tags-from-ec2-meta-data-paths" normalize:"list"`
	TagsFromEC2Tags             bool     `cli:"tags-from-ec2-tags"`
	TagsFromGCPMetaData         bool     `cli:"tags-from-gcp-meta-data"`
	TagsFromGCPMetaDataPaths    []string `cli:"tags-from-gcp-meta-data-paths" normalize:"list"`
	TagsFromGCPLabels           bool     `cli:"tags-from-gcp-labels"`
	TagsFromHost                bool     `cli:"tags-from-host"`
	WaitForEC2TagsTimeout       string   `cli:"wait-for-ec2-tags-timeout"`
	WaitForEC2MetaDataTimeout   string   `cli:"wait-for-ec2-meta-data-timeout"`
	WaitForGCPLabelsTimeout     string   `cli:"wait-for-gcp-labels-timeout"`
	GitCloneFlags               string   `cli:"git-clone-flags"`
	GitCloneMirrorFlags         string   `cli:"git-clone-mirror-flags"`
	GitCleanFlags               string   `cli:"git-clean-flags"`
	GitFetchFlags               string   `cli:"git-fetch-flags"`
	GitMirrorsPath              string   `cli:"git-mirrors-path" normalize:"filepath"`
	GitMirrorsLockTimeout       int      `cli:"git-mirrors-lock-timeout"`
	GitMirrorsSkipUpdate        bool     `cli:"git-mirrors-skip-update"`
	NoGitSubmodules             bool     `cli:"no-git-submodules"`
	NoSSHKeyscan                bool     `cli:"no-ssh-keyscan"`
	NoCommandEval               bool     `cli:"no-command-eval"`
	NoLocalHooks                bool     `cli:"no-local-hooks"`
	NoPlugins                   bool     `cli:"no-plugins"`
	NoPluginValidation          bool     `cli:"no-plugin-validation"`
	NoPTY                       bool     `cli:"no-pty"`
	NoFeatureReporting          bool     `cli:"no-feature-reporting"`
	TimestampLines              bool     `cli:"timestamp-lines"`
	HealthCheckAddr             string   `cli:"health-check-addr"`
	MetricsDatadog              bool     `cli:"metrics-datadog"`
	MetricsDatadogHost          string   `cli:"metrics-datadog-host"`
	MetricsDatadogDistributions bool     `cli:"metrics-datadog-distributions"`
	TracingBackend              string   `cli:"tracing-backend"`
	TracingServiceName          string   `cli:"tracing-service-name"`
	Spawn                       int      `cli:"spawn"`
	SpawnWithPriority           bool     `cli:"spawn-with-priority"`
	LogFormat                   string   `cli:"log-format"`
	CancelSignal                string   `cli:"cancel-signal"`
	RedactedVars                []string `cli:"redacted-vars" normalize:"list"`

	GlobalConfig
	APIConfig
	DeprecatedConfig
}

func (asc AgentStartConfig) Features() []string {
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

	for _, exp := range experiments.Enabled() {
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
		return `/usr/local/bin/bash -e -c`
	case "netbsd":
		return `/usr/pkg/bin/bash -e -c`
	default:
		return `/bin/bash -e -c`
	}
}

func DefaultConfigFilePaths() (paths []string) {
	// Toggle beetwen windows and *nix paths
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

	return
}

var AgentStartCommand = cli.Command{
	Name:        "start",
	Usage:       "Starts a Buildkite agent",
	Description: StartDescription,
	Flags: flatten(
		[]cli.Flag{
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
			cli.IntFlag{
				Name:   "cancel-grace-period",
				Value:  10,
				Usage:  "The number of seconds a canceled or timed out job is given to gracefully terminate and upload its artifacts",
				EnvVar: "BUILDKITE_CANCEL_GRACE_PERIOD",
			},
			cli.BoolFlag{
				Name:   "enable-job-log-tmpfile",
				Usage:  "Store the job logs in a temporary file ′BUILDKITE_JOB_LOG_TMPFILE′ that is accessible during the job and removed at the end of the job",
				EnvVar: "BUILDKITE_ENABLE_JOB_LOG_TMPFILE",
			},
			cli.StringFlag{
				Name:   "shell",
				Value:  DefaultShell(),
				Usage:  "The shell command used to interpret build commands, e.g /bin/bash -e -c",
				EnvVar: "BUILDKITE_SHELL",
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
				Name:   "wait-for-gcp-labels-timeout",
				Usage:  "The amount of time to wait for labels from GCP before proceeding",
				EnvVar: "BUILDKITE_AGENT_WAIT_FOR_GCP_LABELS_TIMEOUT",
				Value:  time.Second * 10,
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
				Value:  "-v --prune",
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
				Name:   "plugins-path",
				Value:  "",
				Usage:  "Directory where the plugins are saved to",
				EnvVar: "BUILDKITE_PLUGINS_PATH",
			},
			cli.BoolFlag{
				Name:   "timestamp-lines",
				Usage:  "Prepend timestamps on each line of output.",
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
				Name:   "metrics-datadog",
				Usage:  "Send metrics to DogStatsD for Datadog",
				EnvVar: "BUILDKITE_METRICS_DATADOG",
			},
			cli.BoolFlag{
				Name:   "no-feature-reporting",
				Usage:  "Disables sending a list of enabled features back to the Buildkite mothership. We use this information to measure feature usage, but if you're not comfortable sharing that information then that's totally okay :)",
				EnvVar: "BUILDKITE_AGENT_NO_FEATURE_REPORTING",
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
				Usage:  "The number of agents to spawn in parallel",
				Value:  1,
				EnvVar: "BUILDKITE_AGENT_SPAWN",
			},
			cli.BoolFlag{
				Name:   "spawn-with-priority",
				Usage:  "Assign priorities to every spawned agent (when using --spawn) equal to the agent's index",
				EnvVar: "BUILDKITE_AGENT_SPAWN_WITH_PRIORITY",
			},
			cli.StringFlag{
				Name:   "cancel-signal",
				Usage:  "The signal to use for cancellation",
				EnvVar: "BUILDKITE_CANCEL_SIGNAL",
				Value:  "SIGTERM",
			},
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
		},
		apiFlags,
		globalFlags,
		deprecatedFlags),

	Action: newCommand(func(cc commandConfig[AgentStartConfig]) {
		ctx := context.Background()
		// Remove any config env from the environment to prevent them propagating to bootstrap
		err := UnsetConfigFromEnvironment(cc.cliContext)
		if err != nil {
			fmt.Printf("%s", err)
			os.Exit(1)
		}

		// Check if git-mirrors are enabled
		if experiments.IsEnabled(`git-mirrors`) {
			if cc.config.GitMirrorsPath == `` {
				cc.logger.Fatal("Must provide a git-mirrors-path in your configuration for git-mirrors experiment")
			}
		}

		// Force some settings if on Windows (these aren't supported yet)
		if runtime.GOOS == "windows" {
			cc.config.NoPTY = true
		}

		// Set a useful default for the bootstrap script
		if cc.config.BootstrapScript == "" {
			exePath, err := os.Executable()
			if err != nil {
				cc.logger.Fatal("Unable to find executable path for bootstrap")
			}
			cc.config.BootstrapScript = fmt.Sprintf("%s bootstrap", shellwords.Quote(exePath))
		}

		isSetNoPlugins := cc.cliContext.IsSet("no-plugins")
		if cc.configLoader.File != nil {
			if _, exists := cc.configLoader.File.Config["no-plugins"]; exists {
				isSetNoPlugins = true
			}
		}

		// Show a warning if plugins are enabled by no-command-eval or no-local-hooks is set
		if isSetNoPlugins && !cc.config.NoPlugins {
			msg := `Plugins have been specifically enabled, despite %s being enabled. ` +
				`Plugins can execute arbitrary hooks and commands, make sure you are ` +
				`whitelisting your plugins in ` +
				`your environment hook.`

			switch {
			case cc.config.NoCommandEval:
				cc.logger.Warn(msg, `no-command-eval`)
			case cc.config.NoLocalHooks:
				cc.logger.Warn(msg, `no-local-hooks`)
			}
		}

		// Turning off command eval or local hooks will also turn off plugins unless
		// `--no-plugins=false` is provided specifically
		if (cc.config.NoCommandEval || cc.config.NoLocalHooks) && !isSetNoPlugins {
			cc.config.NoPlugins = true
		}

		// Guess the shell if none is provided
		if cc.config.Shell == "" {
			cc.config.Shell = DefaultShell()
		}

		// Handle deprecated DisconnectAfterJobTimeout
		if cc.config.DisconnectAfterJobTimeout > 0 {
			cc.config.DisconnectAfterIdleTimeout = cc.config.DisconnectAfterJobTimeout
		}

		var ec2TagTimeout time.Duration
		if t := cc.config.WaitForEC2TagsTimeout; t != "" {
			var err error
			ec2TagTimeout, err = time.ParseDuration(t)
			if err != nil {
				cc.logger.Fatal("Failed to parse ec2 tag timeout: %v", err)
			}
		}

		var ec2MetaDataTimeout time.Duration
		if t := cc.config.WaitForEC2MetaDataTimeout; t != "" {
			var err error
			ec2MetaDataTimeout, err = time.ParseDuration(t)
			if err != nil {
				cc.logger.Fatal("Failed to parse ec2 meta-data timeout: %v", err)
			}
		}

		var gcpLabelsTimeout time.Duration
		if t := cc.config.WaitForGCPLabelsTimeout; t != "" {
			var err error
			gcpLabelsTimeout, err = time.ParseDuration(t)
			if err != nil {
				cc.logger.Fatal("Failed to parse gcp labels timeout: %v", err)
			}
		}

		mc := metrics.NewCollector(cc.logger, metrics.CollectorConfig{
			Datadog:              cc.config.MetricsDatadog,
			DatadogHost:          cc.config.MetricsDatadogHost,
			DatadogDistributions: cc.config.MetricsDatadogDistributions,
		})

		// Sense check supported tracing backends, we don't want bootstrapped jobs to silently have no tracing
		if _, has := tracetools.ValidTracingBackends[cc.config.TracingBackend]; !has {
			cc.logger.Fatal("The given tracing backend %q is not supported. Valid backends are: %q", cc.config.TracingBackend, maps.Keys(tracetools.ValidTracingBackends))
		}

		// AgentConfiguration is the runtime configuration for an agent
		agentConf := agent.AgentConfiguration{
			BootstrapScript:            cc.config.BootstrapScript,
			BuildPath:                  cc.config.BuildPath,
			GitMirrorsPath:             cc.config.GitMirrorsPath,
			GitMirrorsLockTimeout:      cc.config.GitMirrorsLockTimeout,
			GitMirrorsSkipUpdate:       cc.config.GitMirrorsSkipUpdate,
			HooksPath:                  cc.config.HooksPath,
			PluginsPath:                cc.config.PluginsPath,
			GitCloneFlags:              cc.config.GitCloneFlags,
			GitCloneMirrorFlags:        cc.config.GitCloneMirrorFlags,
			GitCleanFlags:              cc.config.GitCleanFlags,
			GitFetchFlags:              cc.config.GitFetchFlags,
			GitSubmodules:              !cc.config.NoGitSubmodules,
			SSHKeyscan:                 !cc.config.NoSSHKeyscan,
			CommandEval:                !cc.config.NoCommandEval,
			PluginsEnabled:             !cc.config.NoPlugins,
			PluginValidation:           !cc.config.NoPluginValidation,
			LocalHooksEnabled:          !cc.config.NoLocalHooks,
			RunInPty:                   !cc.config.NoPTY,
			TimestampLines:             cc.config.TimestampLines,
			DisconnectAfterJob:         cc.config.DisconnectAfterJob,
			DisconnectAfterIdleTimeout: cc.config.DisconnectAfterIdleTimeout,
			CancelGracePeriod:          cc.config.CancelGracePeriod,
			EnableJobLogTmpfile:        cc.config.EnableJobLogTmpfile,
			Shell:                      cc.config.Shell,
			RedactedVars:               cc.config.RedactedVars,
			AcquireJob:                 cc.config.AcquireJob,
			TracingBackend:             cc.config.TracingBackend,
			TracingServiceName:         cc.config.TracingServiceName,
		}

		if cc.configLoader.File != nil {
			agentConf.ConfigPath = cc.configLoader.File.Path
		}

		if cc.config.LogFormat == `text` {
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

			if !cc.config.NoColor {
				fmt.Fprintf(os.Stderr, welcomeMessage, "\x1b[38;5;48m", "\x1b[0m")
			} else {
				fmt.Fprintf(os.Stderr, welcomeMessage, "", "")
			}
		}

		cc.logger.Notice("Starting buildkite-agent v%s with PID: %s", agent.Version(), fmt.Sprintf("%d", os.Getpid()))
		cc.logger.Notice("The agent source code can be found here: https://github.com/buildkite/agent")
		cc.logger.Notice("For questions and support, email us at: hello@buildkite.com")

		if agentConf.ConfigPath != "" {
			cc.logger.WithFields(logger.StringField(`path`, agentConf.ConfigPath)).Info("Configuration loaded")
		}

		cc.logger.Debug("Bootstrap command: %s", agentConf.BootstrapScript)
		cc.logger.Debug("Build path: %s", agentConf.BuildPath)
		cc.logger.Debug("Hooks directory: %s", agentConf.HooksPath)
		cc.logger.Debug("Plugins directory: %s", agentConf.PluginsPath)

		if !agentConf.SSHKeyscan {
			cc.logger.Info("Automatic ssh-keyscan has been disabled")
		}

		if !agentConf.CommandEval {
			cc.logger.Info("Evaluating console commands has been disabled")
		}

		if !agentConf.PluginsEnabled {
			cc.logger.Info("Plugins have been disabled")
		}

		if !agentConf.RunInPty {
			cc.logger.Info("Running builds within a pseudoterminal (PTY) has been disabled")
		}

		if agentConf.DisconnectAfterJob {
			cc.logger.Info("Agents will disconnect after a job run has completed")
		}

		if agentConf.DisconnectAfterIdleTimeout > 0 {
			cc.logger.Info("Agents will disconnect after %d seconds of inactivity", agentConf.DisconnectAfterIdleTimeout)
		}

		cancelSig, err := process.ParseSignal(cc.config.CancelSignal)
		if err != nil {
			cc.logger.Fatal("Failed to parse cancel-signal: %v", err)
		}

		// confirm the BuildPath is exists. The bootstrap is going to write to it when a job executes,
		// so we may as well check that'll work now and fail early if it's a problem
		if !utils.FileExists(agentConf.BuildPath) {
			cc.logger.Info("Build Path doesn't exist, creating it (%s)", agentConf.BuildPath)
			// Actual file permissions will be reduced by umask, and won't be 0777 unless the user has manually changed the umask to 000
			if err := os.MkdirAll(agentConf.BuildPath, 0777); err != nil {
				cc.logger.Fatal("Failed to create builds path: %v", err)
			}
		}

		// Create the API client
		client := api.NewClient(cc.logger, loadAPIClientConfig(cc.config, `Token`))

		// The registration request for all agents
		registerReq := api.AgentRegisterRequest{
			Name:              cc.config.Name,
			Priority:          cc.config.Priority,
			ScriptEvalEnabled: !cc.config.NoCommandEval,
			Tags: agent.FetchTags(ctx, cc.logger, agent.FetchTagsConfig{
				Tags:                      cc.config.Tags,
				TagsFromEC2MetaData:       (cc.config.TagsFromEC2MetaData || cc.config.TagsFromEC2),
				TagsFromEC2MetaDataPaths:  cc.config.TagsFromEC2MetaDataPaths,
				TagsFromEC2Tags:           cc.config.TagsFromEC2Tags,
				TagsFromGCPMetaData:       (cc.config.TagsFromGCPMetaData || cc.config.TagsFromGCP),
				TagsFromGCPMetaDataPaths:  cc.config.TagsFromGCPMetaDataPaths,
				TagsFromGCPLabels:         cc.config.TagsFromGCPLabels,
				TagsFromHost:              cc.config.TagsFromHost,
				WaitForEC2TagsTimeout:     ec2TagTimeout,
				WaitForEC2MetaDataTimeout: ec2MetaDataTimeout,
				WaitForGCPLabelsTimeout:   gcpLabelsTimeout,
			}),
			// We only want this agent to be ingored in Buildkite
			// dispatches if it's being booted to acquire a
			// specific job.
			IgnoreInDispatches: cc.config.AcquireJob != "",
			Features:           cc.config.Features(),
		}

		// Spawning multiple agents doesn't work if the agent is being
		// booted in acquisition mode
		if cc.config.Spawn > 1 && cc.config.AcquireJob != "" {
			cc.logger.Fatal("You can't spawn multiple agents and acquire a job at the same time")
		}

		var workers []*agent.AgentWorker

		for i := 1; i <= cc.config.Spawn; i++ {
			if cc.config.Spawn == 1 {
				cc.logger.Info("Registering agent with Buildkite...")
			} else {
				cc.logger.Info("Registering agent %d of %d with Buildkite...", i, cc.config.Spawn)
			}

			// Handle per-spawn name interpolation, replacing %spawn with the spawn index
			registerReq.Name = strings.ReplaceAll(cc.config.Name, "%spawn", strconv.Itoa(i))

			if cc.config.SpawnWithPriority {
				cc.logger.Info("Assigning priority %s for agent %d", strconv.Itoa(i), i)
				registerReq.Priority = strconv.Itoa(i)
			}

			// Register the agent with the buildkite API
			ag, err := agent.Register(ctx, cc.logger, client, registerReq)
			if err != nil {
				cc.logger.Fatal("%s", err)
			}

			// Create an agent worker to run the agent
			workers = append(workers,
				agent.NewAgentWorker(
					cc.logger.WithFields(logger.StringField(`agent`, ag.Name)), ag, mc, client, agent.AgentWorkerConfig{
						AgentConfiguration: agentConf,
						CancelSignal:       cancelSig,
						Debug:              cc.config.Debug,
						DebugHTTP:          cc.config.DebugHTTP,
						SpawnIndex:         i,
					}))
		}

		// Setup the agent pool that spawns agent workers
		pool := agent.NewAgentPool(workers)

		// Agent-wide shutdown hook. Once per agent, for all workers on the agent.
		defer agentShutdownHook(cc.logger, cc.config)

		// Handle process signals
		signals := handlePoolSignals(cc.logger, pool)
		defer close(signals)

		cc.logger.Info("Starting %d Agent(s)", cc.config.Spawn)
		cc.logger.Info("You can press Ctrl-c to stop the agents")

		// Determine the health check listening address and port for this agent
		if cc.config.HealthCheckAddr != "" {
			http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/" {
					http.NotFound(w, r)
				} else {
					fmt.Fprintf(w, "OK: Buildkite agent is running")
				}
			})

			go func() {
				cc.logger.Notice("Starting HTTP health check server on %v", cc.config.HealthCheckAddr)
				err := http.ListenAndServe(cc.config.HealthCheckAddr, nil)
				if err != nil {
					cc.logger.Error("Could not start health check server: %v", err)
				}
			}()
		}

		// Start the agent pool
		if err := pool.Start(ctx); err != nil {
			cc.logger.Fatal("%s", err)
		}
	}),
}

func handlePoolSignals(l logger.Logger, pool *agent.AgentPool) chan os.Signal {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGQUIT)

	go func() {
		var interruptCount int

		for sig := range signals {
			l.Debug("Received signal `%v`", sig)

			switch sig {
			case syscall.SIGQUIT:
				l.Debug("Received signal `%s`", sig.String())
				pool.Stop(false)
			case syscall.SIGTERM, syscall.SIGINT:
				l.Debug("Received signal `%s`", sig.String())
				if interruptCount == 0 {
					interruptCount++
					l.Info("Received CTRL-c, send again to forcefully kill the agent(s)")
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

// agentShutdownHook looks for an agent-shutdown hook script in the hooks path
// and executes it if found. Output (stdout + stderr) is streamed into the main
// agent logger. Exit status failure is logged but ignored.
func agentShutdownHook(log logger.Logger, cfg AgentStartConfig) {
	// search for agent-shutdown hook (including .bat & .ps1 files on Windows)
	p, err := hook.Find(cfg.HooksPath, "agent-shutdown")
	if err != nil {
		if !os.IsNotExist(err) {
			log.Error("Error finding agent-shutdown hook: %v", err)
		}
		return
	}
	sh, err := shell.New()
	if err != nil {
		log.Error("creating shell for agent-shutdown hook: %v", err)
		return
	}

	// pipe from hook output to logger
	r, w := io.Pipe()
	sh.Logger = &shell.WriterLogger{Writer: w, Ansi: !cfg.NoColor} // for Promptf
	sh.Writer = w                                                  // for stdout+stderr
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		scan := bufio.NewScanner(r) // log each line separately
		log = log.WithFields(logger.StringField("hook", "agent-shutdown"))
		for scan.Scan() {
			log.Info(scan.Text())
		}
	}()

	// run agent-shutdown hook
	sh.Promptf("%s", p)
	if err = sh.RunScript(context.Background(), p, nil); err != nil {
		log.Error("agent-shutdown hook: %v", err)
	}
	w.Close() // goroutine scans until pipe is closed

	// wait for hook to finish and output to flush to logger
	wg.Wait()
}
