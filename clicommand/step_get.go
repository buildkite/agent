package clicommand

import (
	"fmt"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/retry"
	"github.com/urfave/cli"
)

var StepGetHelpDescription = `Usage:

   buildkite-agent step get <attribute> [options...]

Description:

   Retrieve the value of an attribute in a step. If no attribute is passed, the
   entire step will be returned.

   In the event a complex object is returned (i.e. an object or an array),
   you'll need to supply the --format option to tell the agent how it should
   output the data (currently only JSON is supported).

Example:

   $ buildkite-agent step get "label"
   $ buildkite-agent step get --format json
   $ buildkite-agent step get "retry" --format json
   $ buildkite-agent step get "state" --step "my-other-step"`

type StepGetConfig struct {
	Attribute string `cli:"arg:0" label:"step attribute"`
	StepOrKey string `cli:"step" validate:"required"`
	Build     string `cli:"build"`
	Format    string `cli:"format"`

	// Global flags
	Debug       bool     `cli:"debug"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`

	// API config
	DebugHTTP        bool   `cli:"debug-http"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoHTTP2          bool   `cli:"no-http2"`
}

var StepGetCommand = cli.Command{
	Name:        "get",
	Usage:       "Get the value of an attribute",
	Description: StepGetHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "step",
			Value:  "",
			Usage:  "The step to get. Can be either its ID (BUILDKITE_STEP_ID) or key (BUILDKITE_STEP_KEY)",
			EnvVar: "BUILDKITE_STEP_ID",
		},
		cli.StringFlag{
			Name:   "build",
			Value:  "",
			Usage:  "The build to look for the step in. Only required when targeting a step using its key (BUILDKITE_STEP_KEY)",
			EnvVar: "BUILDKITE_BUILD_ID",
		},
		cli.StringFlag{
			Name:   "format",
			Value:  "",
			Usage:  "The format to output the attribute value in (currently only JSON is supported)",
			EnvVar: "BUILDKITE_STEP_GET_FORMAT",
		},

		// API Flags
		AgentAccessTokenFlag,
		EndpointFlag,
		NoHTTP2Flag,
		DebugHTTPFlag,

		// Global flags
		NoColorFlag,
		DebugFlag,
		ExperimentsFlag,
		ProfileFlag,
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := StepGetConfig{}

		l := CreateLogger(&cfg)

		// Load the configuration
		if err := cliconfig.Load(c, l, &cfg); err != nil {
			l.Fatal("%s", err)
		}

		// Setup any global configuration options
		done := HandleGlobalFlags(l, cfg)
		defer done()

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, `AgentAccessToken`))

		// Create the request
		stepExportRequest := &api.StepExportRequest{
			Build:     cfg.Build,
			Attribute: cfg.Attribute,
			Format:    cfg.Format,
		}

		// Find the step attribute
		var err error
		var resp *api.Response
		var stepExportResponse *api.StepExportResponse
		err = retry.Do(func(s *retry.Stats) error {
			stepExportResponse, resp, err = client.StepExport(cfg.StepOrKey, stepExportRequest)
			// Don't bother retrying if the response was one of these statuses
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404 || resp.StatusCode == 400) {
				s.Break()
				return err
			}
			if err != nil {
				l.Warn("%s (%s)", err, s)
			}

			return err
		}, &retry.Config{Maximum: 10, Interval: 5 * time.Second})

		// Deal with the error if we got one
		if err != nil {
			l.Fatal("Failed to get step: %s", err)
		}

		// Output the value to STDOUT
		fmt.Print(stepExportResponse.Output)
	},
}
