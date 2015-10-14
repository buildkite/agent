package clicommand

import (
	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/codegangsta/cli"
)

var BootstrapHelpDescription = `Usage:

   buildkite-agent bootstrap [arguments...]

Description:

   The bootstrap command checks out the jobs repository source code and
   executes the commands defined in the job.

   It handles hooks, plugins artifacts for the job.

Example:

   $ export $(curl -s -H "Authorization: Bearer xxx" \
     "https://api.buildkite.com/v1/organizations/[org]/projects/[proj]/builds/[build]/jobs/[job]/env.txt" | xargs)
   $ buildkite-agent bootstrap`

type BootstrapConfig struct {
	Repository                   string `cli:"repository"`
	Commit                       string `cli:"commit"`
	Branch                       string `cli:"branch"`
	AgentName                    string `cli:"agent"`
	PipelineSlug                 string `cli:"pipeline"`
	ProjectSlug                  string `cli:"project"`
	AutomaticArtifactUploadPaths string `cli:"artifact-upload-paths"`
	ArtifactUploadDestination    string `cli:"artifact-upload-destination"`
	CleanCheckout                bool   `cli:"clean-checkout"`
	BinPath                      string `cli:"bin-path" normalize:"filepath"`
	BuildPath                    string `cli:"build-path" normalize:"filepath"`
	HooksPath                    string `cli:"hooks-path" normalize:"filepath"`
	NoPTY                        bool   `cli:"no-pty"`
	Debug                        bool   `cli:"debug"`
}

var BootstrapCommand = cli.Command{
	Name:        "bootstrap",
	Usage:       "Run a Buildkite job locally",
	Description: BootstrapHelpDescription,
	Flags: []cli.Flag{
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
			Name:   "agent",
			Value:  "",
			Usage:  "The name of the agent running the job",
			EnvVar: "BUILDKITE_AGENT_NAME",
		},
		cli.StringFlag{
			Name:   "pipeline",
			Value:  "",
			Usage:  "The ID of the pipeline that the job is a part of",
			EnvVar: "BUILDKITE_PIPELINE",
		},
		cli.StringFlag{
			Name:   "project",
			Value:  "",
			Usage:  "The slug of the project that the job is a part of [DEPRECATED]",
			EnvVar: "BUILDKITE_PROJECT_SLUG",
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
		cli.BoolFlag{
			Name:   "no-pty",
			Usage:  "Do not run jobs within a pseudo terminal",
			EnvVar: "BUILDKITE_NO_PTY",
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

		// Support the deprecated --project-slug option
		var pipelineSlug string
		if cfg.PipelineSlug != "" {
			pipelineSlug = cfg.PipelineSlug
		} else {
			pipelineSlug = cfg.ProjectSlug
		}

		// Start the bootstraper
		agent.Bootstrap{
			Repository:                   cfg.Repository,
			Commit:                       cfg.Commit,
			Branch:                       cfg.Branch,
			AgentName:                    cfg.AgentName,
			PipelineSlug:                 pipelineSlug,
			AutomaticArtifactUploadPaths: cfg.AutomaticArtifactUploadPaths,
			ArtifactUploadDestination:    cfg.ArtifactUploadDestination,
			CleanCheckout:                cfg.CleanCheckout,
			BuildPath:                    cfg.BuildPath,
			BinPath:                      cfg.BinPath,
			HooksPath:                    cfg.HooksPath,
			Debug:                        cfg.Debug,
			RunInPty:                     !cfg.NoPTY,
		}.Start()
	},
}
