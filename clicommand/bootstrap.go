package clicommand

import (
	"os"
	"runtime"

	"github.com/buildkite/agent/bootstrap"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/urfave/cli"
)

var BootstrapHelpDescription = `Usage:

   buildkite-agent bootstrap [arguments...]

Description:

   The bootstrap command checks out the jobs repository source code and
   executes the commands defined in the job.

Example:

   $ eval $(curl -s -H "Authorization: Bearer xxx" \
     "https://api.buildkite.com/v2/organizations/[org]/pipelines/[proj]/builds/[build]/jobs/[job]/env.txt" | sed 's/^/export /')
   $ buildkite-agent bootstrap --build-path builds`

type BootstrapConfig struct {
	Command                      string `cli:"command"`
	JobID                        string `cli:"job" validate:"required"`
	Repository                   string `cli:"repository" validate:"required"`
	Commit                       string `cli:"commit" validate:"required"`
	Branch                       string `cli:"branch" validate:"required"`
	Tag                          string `cli:"tag"`
	RefSpec                      string `cli:"refspec"`
	Plugins                      string `cli:"plugins"`
	PullRequest                  string `cli:"pullrequest"`
	GitSubmodules                bool   `cli:"git-submodules"`
	SSHKeyscan                   bool   `cli:"ssh-keyscan"`
	AgentName                    string `cli:"agent" validate:"required"`
	OrganizationSlug             string `cli:"organization" validate:"required"`
	PipelineSlug                 string `cli:"pipeline" validate:"required"`
	PipelineProvider             string `cli:"pipeline-provider" validate:"required"`
	AutomaticArtifactUploadPaths string `cli:"artifact-upload-paths"`
	ArtifactUploadDestination    string `cli:"artifact-upload-destination"`
	CleanCheckout                bool   `cli:"clean-checkout"`
	GitCloneFlags                string `cli:"git-clone-flags"`
	GitCleanFlags                string `cli:"git-clean-flags"`
	BinPath                      string `cli:"bin-path" normalize:"filepath"`
	BuildPath                    string `cli:"build-path" normalize:"filepath" validate:"required"`
	HooksPath                    string `cli:"hooks-path" normalize:"filepath"`
	PluginsPath                  string `cli:"plugins-path" normalize:"filepath"`
	CommandEval                  bool   `cli:"command-eval"`
	PluginsEnabled               bool   `cli:"plugins-enabled"`
	PTY                          bool   `cli:"pty"`
	Debug                        bool   `cli:"debug"`
	Shell                        string `cli:"shell"`
}

var BootstrapCommand = cli.Command{
	Name:        "bootstrap",
	Usage:       "Run a Buildkite job locally",
	Description: BootstrapHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "command",
			Value:  "",
			Usage:  "The command to run",
			EnvVar: "BUILDKITE_COMMAND",
		},
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "The ID of the job being run",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		cli.StringFlag{
			Name:   "repository",
			Value:  "",
			Usage:  "The repository to clone and run the job from",
			EnvVar: "BUILDKITE_REPO",
		},
		cli.StringFlag{
			Name:   "commit",
			Value:  "",
			Usage:  "The commit to checkout in the repository",
			EnvVar: "BUILDKITE_COMMIT",
		},
		cli.StringFlag{
			Name:   "branch",
			Value:  "",
			Usage:  "The branch the commit is in",
			EnvVar: "BUILDKITE_BRANCH",
		},
		cli.StringFlag{
			Name:   "tag",
			Value:  "",
			Usage:  "The tag the commit",
			EnvVar: "BUILDKITE_TAG",
		},
		cli.StringFlag{
			Name:   "refspec",
			Value:  "",
			Usage:  "Optional refspec to override git fetch",
			EnvVar: "BUILDKITE_REFSPEC",
		},
		cli.StringFlag{
			Name:   "plugins",
			Value:  "",
			Usage:  "The plugins for the job",
			EnvVar: "BUILDKITE_PLUGINS",
		},
		cli.StringFlag{
			Name:   "pullrequest",
			Value:  "",
			Usage:  "The number/id of the pull request this commit belonged to",
			EnvVar: "BUILDKITE_PULL_REQUEST",
		},
		cli.StringFlag{
			Name:   "agent",
			Value:  "",
			Usage:  "The name of the agent running the job",
			EnvVar: "BUILDKITE_AGENT_NAME",
		},
		cli.StringFlag{
			Name:   "organization",
			Value:  "",
			Usage:  "The slug of the organization that the job is a part of",
			EnvVar: "BUILDKITE_ORGANIZATION_SLUG",
		},
		cli.StringFlag{
			Name:   "pipeline",
			Value:  "",
			Usage:  "The slug of the pipeline that the job is a part of",
			EnvVar: "BUILDKITE_PIPELINE_SLUG",
		},
		cli.StringFlag{
			Name:   "pipeline-provider",
			Value:  "",
			Usage:  "The id of the SCM provider that the repository is hosted on",
			EnvVar: "BUILDKITE_PIPELINE_PROVIDER",
		},
		cli.StringFlag{
			Name:   "artifact-upload-paths",
			Value:  "",
			Usage:  "Paths to files to automatically upload at the end of a job",
			EnvVar: "BUILDKITE_ARTIFACT_PATHS",
		},
		cli.StringFlag{
			Name:   "artifact-upload-destination",
			Value:  "",
			Usage:  "A custom location to upload artifact paths to (i.e. s3://my-custom-bucket)",
			EnvVar: "BUILDKITE_ARTIFACT_UPLOAD_DESTINATION",
		},
		cli.BoolFlag{
			Name:   "clean-checkout",
			Usage:  "Whether or not the bootstrap should remove the existing repository before running the command",
			EnvVar: "BUILDKITE_CLEAN_CHECKOUT",
		},
		cli.StringFlag{
			Name:   "git-clone-flags",
			Value:  "-v",
			Usage:  "Flags to pass to \"git clone\" command",
			EnvVar: "BUILDKITE_GIT_CLONE_FLAGS",
		},
		cli.StringFlag{
			Name:   "git-clean-flags",
			Value:  "-fxdq",
			Usage:  "Flags to pass to \"git clean\" command",
			EnvVar: "BUILDKITE_GIT_CLEAN_FLAGS",
		},
		cli.StringFlag{
			Name:   "bin-path",
			Value:  "",
			Usage:  "Directory where the buildkite-agent binary lives",
			EnvVar: "BUILDKITE_BIN_PATH",
		},
		cli.StringFlag{
			Name:   "build-path",
			Value:  "",
			Usage:  "Directory where builds will be created",
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
		cli.BoolTFlag{
			Name:   "command-eval",
			Usage:  "Allow running of arbitary commands",
			EnvVar: "BUILDKITE_COMMAND_EVAL",
		},
		cli.BoolTFlag{
			Name:   "plugins-enabled",
			Usage:  "Allow plugins to be run",
			EnvVar: "BUILDKITE_PLUGINS_ENABLED",
		},
		cli.BoolTFlag{
			Name:   "ssh-keyscan",
			Usage:  "Automatically run ssh-keyscan before checkout",
			EnvVar: "BUILDKITE_SSH_KEYSCAN",
		},
		cli.BoolTFlag{
			Name:   "git-submodules",
			Usage:  "Enable git submodules",
			EnvVar: "BUILDKITE_GIT_SUBMODULES",
		},
		cli.BoolTFlag{
			Name:   "pty",
			Usage:  "Run jobs within a pseudo terminal",
			EnvVar: "BUILDKITE_PTY",
		},
		cli.StringFlag{
			Name:   "shell",
			Usage:  "The shell to use to interpret build commands",
			EnvVar: "BUILDKITE_SHELL",
			Value:  DefaultShell(),
		},
		DebugFlag,
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := BootstrapConfig{}

		// Load the configuration
		if err := cliconfig.Load(c, &cfg); err != nil {
			logger.Fatal("%s", err)
		}

		// Turn of PTY support if we're on Windows
		runInPty := cfg.PTY
		if runtime.GOOS == "windows" {
			runInPty = false
		}

		// Configure the bootstraper
		bootstrap := &bootstrap.Bootstrap{
			Config: bootstrap.Config{
				Command:                      cfg.Command,
				JobID:                        cfg.JobID,
				Repository:                   cfg.Repository,
				Commit:                       cfg.Commit,
				Branch:                       cfg.Branch,
				Tag:                          cfg.Tag,
				RefSpec:                      cfg.RefSpec,
				Plugins:                      cfg.Plugins,
				GitSubmodules:                cfg.GitSubmodules,
				PullRequest:                  cfg.PullRequest,
				GitCloneFlags:                cfg.GitCloneFlags,
				GitCleanFlags:                cfg.GitCleanFlags,
				AgentName:                    cfg.AgentName,
				PipelineProvider:             cfg.PipelineProvider,
				PipelineSlug:                 cfg.PipelineSlug,
				OrganizationSlug:             cfg.OrganizationSlug,
				AutomaticArtifactUploadPaths: cfg.AutomaticArtifactUploadPaths,
				ArtifactUploadDestination:    cfg.ArtifactUploadDestination,
				CleanCheckout:                cfg.CleanCheckout,
				BuildPath:                    cfg.BuildPath,
				BinPath:                      cfg.BinPath,
				HooksPath:                    cfg.HooksPath,
				PluginsPath:                  cfg.PluginsPath,
				Debug:                        cfg.Debug,
				RunInPty:                     runInPty,
				CommandEval:                  cfg.CommandEval,
				PluginsEnabled:               cfg.PluginsEnabled,
				SSHKeyscan:                   cfg.SSHKeyscan,
				Shell:                        cfg.Shell,
			},
		}

		// Run the bootstrap and exit with whatever it returns
		os.Exit(bootstrap.Start())
	},
}
