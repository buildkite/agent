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
	Debug     bool   `cli:"debug"`
	HooksPath string `cli:"hooks-path" normalize:"filepath"`
	NoPTY     bool   `cli:"no-pty"`
}

var BootstrapCommand = cli.Command{
	Name:        "bootstrap",
	Usage:       "Run a Buildkite job locally",
	Description: BootstrapHelpDescription,
	Flags: []cli.Flag{
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

		// Start the bootstraper
		agent.Bootstrap{
			HooksPath: cfg.HooksPath,
			Debug:     cfg.Debug,
			RunInPty:  !cfg.NoPTY,
		}.Start()
	},
}
