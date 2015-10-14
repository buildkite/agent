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
	Tag                          string `cli:"tag"`
	PullRequest                  string `cli:"pullrequest"`
	NoGitSubmodules              bool   `cli:"no-git-submodules"`
	AgentName                    string `cli:"agent"`
	ProjectSlug                  string `cli:"project"`
	ProjectProvider              string `cli:"project-provider"`
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
			Name:   "tag",
			Value:  "",
			Usage:  "The tag the commit",
			EnvVar: "BUILDKITE_TAG",
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
			Name:   "project",
			Value:  "",
			Usage:  "The slug of the project that the job is a part of",
			EnvVar: "BUILDKITE_PROJECT_SLUG",
		},
		cli.StringFlag{
			Name:   "project-provoder",
			Value:  "",
			Usage:  "The id of the SCM provider that the repository is hosted on",
			EnvVar: "BUILDKITE_PROJECT_PROVIDER",
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
			Name:   "no-git-submodules",
			Usage:  "Disable git submodules",
			EnvVar: "BUILDKITE_DISABLE_GIT_SUBMODULES",
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

		// Start the bootstraper
		agent.Bootstrap{
			Repository:                   cfg.Repository,
			Commit:                       cfg.Commit,
			Branch:                       cfg.Branch,
			Tag:                          cfg.Tag,
			GitSubmodules:                !cfg.NoGitSubmodules,
			PullRequest:                  cfg.PullRequest,
			AgentName:                    cfg.AgentName,
			ProjectProvider:              cfg.ProjectProvider,
			ProjectSlug:                  cfg.ProjectSlug,
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
