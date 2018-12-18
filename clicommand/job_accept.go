package clicommand

import (
	"fmt"
	"os"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/metrics"
	"github.com/buildkite/shellwords"
	"github.com/urfave/cli"
)

var JobAcceptHelpDescription = `Usage:

   buildkite-agent job accept [arguments...]

Description:

   Accept a job, do the thing.

Example:

   $ buildkite-agent job accept --job 1234 --agent-access-token foobar`

type JobAcceptConfig struct {
	JobID              string `cli:"job" validate:"required"`
	AgentAccessToken   string `cli:"agent-access-token" validate:"required"`
	BootstrapScript    string `cli:"bootstrap-script" normalize:"commandpath"`
	BuildPath          string `cli:"build-path" normalize:"filepath" validate:"required"`
	HooksPath          string `cli:"hooks-path" normalize:"filepath"`
	PluginsPath        string `cli:"plugins-path" normalize:"filepath"`
	Shell              string `cli:"shell"`
	GitCloneFlags      string `cli:"git-clone-flags"`
	GitCleanFlags      string `cli:"git-clean-flags"`
	NoGitSubmodules    bool   `cli:"no-git-submodules"`
	NoSSHKeyscan       bool   `cli:"no-ssh-keyscan"`
	NoCommandEval      bool   `cli:"no-command-eval"`
	NoLocalHooks       bool   `cli:"no-local-hooks"`
	NoPlugins          bool   `cli:"no-plugins"`
	NoPluginValidation bool   `cli:"no-plugin-validation"`
	NoPTY              bool   `cli:"no-pty"`
	TimestampLines     bool   `cli:"timestamp-lines"`
	CancelGracePeriod  int    `cli:"cancel-grace-period"`
	Endpoint           string `cli:"endpoint" validate:"required"`
	MetricsDatadog     bool   `cli:"metrics-datadog"`
	MetricsDatadogHost string `cli:"metrics-datadog-host"`
	Debug              bool   `cli:"debug"`
	DebugHTTP          bool   `cli:"debug-http"`
}

var JobAcceptCommand = cli.Command{
	Name:        "accept",
	Usage:       "Accept and run a Buildkite job locally uploading the logs to Buildkite.com",
	Description: JobAcceptHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "The ID of the job being run",
			EnvVar: "BUILDKITE_JOB_ID",
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
		cli.StringFlag{
			Name:   "shell",
			Value:  DefaultShell(),
			Usage:  "The shell commamnd used to interpret build commands, e.g /bin/bash -e -c",
			EnvVar: "BUILDKITE_SHELL",
		},
		cli.StringFlag{
			Name:   "git-clone-flags",
			Value:  "-v",
			Usage:  "Flags to pass to the \"git clone\" command",
			EnvVar: "BUILDKITE_GIT_CLONE_FLAGS",
		},
		cli.StringFlag{
			Name:   "git-clean-flags",
			Value:  "-fxdq",
			Usage:  "Flags to pass to \"git clean\" command",
			EnvVar: "BUILDKITE_GIT_CLEAN_FLAGS",
		},
		cli.BoolFlag{
			Name:   "no-git-submodules",
			Usage:  "Don't automatically checkout git submodules",
			EnvVar: "BUILDKITE_NO_GIT_SUBMODULES,BUILDKITE_DISABLE_GIT_SUBMODULES",
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
			Name:   "timestamp-lines",
			Usage:  "Prepend timestamps on each line of output.",
			EnvVar: "BUILDKITE_TIMESTAMP_LINES",
		},
		cli.IntFlag{
			Name:   "cancel-grace-period",
			Value:  10,
			Usage:  "The number of seconds running processes are given to gracefully terminate before they are killed when a job is cancelled",
			EnvVar: "BUILDKITE_CANCEL_GRACE_PERIOD",
		},
		cli.BoolFlag{
			Name:   "metrics-datadog",
			Usage:  "Send metrics to DogStatsD for Datadog",
			EnvVar: "BUILDKITE_METRICS_DATADOG",
		},
		cli.StringFlag{
			Name:   "metrics-datadog-host",
			Usage:  "The dogstatsd instance to send metrics to via udp",
			EnvVar: "BUILDKITE_METRICS_DATADOG_HOST",
			Value:  "127.0.0.1:8125",
		},
		AgentAccessTokenFlag,
		EndpointFlag,
		DebugFlag,
		DebugHTTPFlag,
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := JobAcceptConfig{}

		// Load the configuration
		if err := cliconfig.Load(c, &cfg); err != nil {
			logger.Fatal("%s", err)
		}

		/*
			Reconstruct the agent and agent configuration:

			Agent needs:

			- access_token
			- job status interval

			Agent Config should be full
		*/

		// Set a useful default for the bootstrap script
		if cfg.BootstrapScript == "" {
			cfg.BootstrapScript = fmt.Sprintf("%s bootstrap", shellwords.Quote(os.Args[0]))
		}

		a := agent.AgentWorker{
			Endpoint: cfg.Endpoint,
			Agent: &api.Agent{
				Endpoint:          cfg.Endpoint,
				AccessToken:       cfg.AgentAccessToken,
				JobStatusInterval: 2,
			},
			AgentConfiguration: &agent.AgentConfiguration{
				BootstrapScript:   cfg.BootstrapScript,
				BuildPath:         cfg.BuildPath,
				HooksPath:         cfg.HooksPath,
				PluginsPath:       cfg.PluginsPath,
				GitCloneFlags:     cfg.GitCloneFlags,
				GitCleanFlags:     cfg.GitCleanFlags,
				GitSubmodules:     !cfg.NoGitSubmodules,
				SSHKeyscan:        !cfg.NoSSHKeyscan,
				CommandEval:       !cfg.NoCommandEval,
				PluginsEnabled:    !cfg.NoPlugins,
				PluginValidation:  !cfg.NoPluginValidation,
				LocalHooksEnabled: !cfg.NoLocalHooks,
				RunInPty:          !cfg.NoPTY,
				TimestampLines:    cfg.TimestampLines,
				CancelGracePeriod: cfg.CancelGracePeriod,
				Shell:             cfg.Shell,
			},
			MetricsCollector: &metrics.Collector{
				Datadog:     cfg.MetricsDatadog,
				DatadogHost: cfg.MetricsDatadogHost,
			},
		}.Create()

		job := &api.Job{
			ID: cfg.JobID,
		}
		job, _, err := a.APIClient.Jobs.Accept(job)
		if err != nil {
			logger.Fatal("couldn't accept job: %s", err)
		}

		// TODO add agent_name metric
		jobMetricsScope := a.MetricsCollector.Scope(metrics.Tags{
			`pipeline`: job.Env[`BUILDKITE_PIPELINE_SLUG`],
			`org`:      job.Env[`BUILDKITE_ORGANIZATION_SLUG`],
			`branch`:   job.Env[`BUILDKITE_BRANCH`],
			`source`:   job.Env[`BUILDKITE_SOURCE`],
		})

		// Configure the job runner
		jobRunner, err := agent.JobRunner{
			Endpoint:           a.Endpoint,
			Agent:              a.Agent,
			AgentConfiguration: a.AgentConfiguration,
			Job:                job,
			Metrics:            jobMetricsScope,
		}.Create()
		if err != nil {
			logger.Fatal("couldn't create job runner: %s", err)
		}

		err = jobRunner.Run()
		if err != nil {
			logger.Fatal("couldn't run job: %s", err)
		}

		os.Exit(0)
	},
}
