package clicommand

import (
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/retry"
	"github.com/urfave/cli"
)

var AnnotationRemoveHelpDescription = `Usage:

   buildkite-agent annotation remove [arguments...]

Description:

   Remove an existing annotation which was previously published using the
   buildkite-agent annotate command.

   If you leave context blank, it will use the default context.

Example:

   $ buildkite-agent annotation remove
   $ buildkite-agent annotation remove --context "remove-me"`

type AnnotationRemoveConfig struct {
	Context string `cli:"context" validate:"required"`
	Job     string `cli:"job" validate:"required"`

	// Global flags
	Debug       bool     `cli:"debug"`
	LogLevel    string   `cli:"log-level"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`

	// API config
	DebugHTTP        bool   `cli:"debug-http"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoHTTP2          bool   `cli:"no-http2"`
}

var AnnotationRemoveCommand = cli.Command{
	Name:        "remove",
	Usage:       "Remove an existing annotation from a Buildkite build",
	Description: AnnotationRemoveHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "context",
			Value:  "default",
			Usage:  "The context of the annotation used to differentiate this annotation from others",
			EnvVar: "BUILDKITE_ANNOTATION_CONTEXT",
		},
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "Which job is removing the annotation",
			EnvVar: "BUILDKITE_JOB_ID",
		},

		// API Flags
		AgentAccessTokenFlag,
		EndpointFlag,
		NoHTTP2Flag,
		DebugHTTPFlag,

		// Global flags
		NoColorFlag,
		DebugFlag,
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := AnnotationRemoveConfig{}

		l := CreateLogger(&cfg)

		// Load the configuration
		if err := cliconfig.Load(c, l, &cfg); err != nil {
			l.Fatal("%s", err)
		}

		// Setup any global configuration options
		done := HandleGlobalFlags(l, cfg)
		defer done()

		var err error

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, `AgentAccessToken`))

		// Retry the removal a few times before giving up
		err = retry.NewRetrier(
			retry.WithMaxAttempts(5),
			retry.WithStrategy(retry.Constant(1*time.Second)),
			retry.WithJitter(),
		).Do(func(r *retry.Retrier) error {
			// Attempt to remove the annotation
			resp, err := client.AnnotationRemove(cfg.Job, cfg.Context)

			// Don't bother retrying if the response was one of these statuses
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404 || resp.StatusCode == 400 || resp.StatusCode == 410) {
				r.Break()
				return err
			}

			// Show the unexpected error
			if err != nil {
				l.Warn("%s (%s)", err, r)
			}

			return err
		})

		// Show a fatal error if we gave up trying to create the annotation
		if err != nil {
			l.Fatal("Failed to remove annotation: %s", err)
		}

		l.Debug("Successfully removed annotation")
	},
}
