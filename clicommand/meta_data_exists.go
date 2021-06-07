package clicommand

import (
	"os"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/retry"
	"github.com/urfave/cli"
)

var MetaDataExistsHelpDescription = `Usage:

   buildkite-agent meta-data exists <key> [options...]

Description:

   The command exits with a status of 0 if the key has been set, or it will
   exit with a status of 100 if the key doesn't exist.

Example:

   $ buildkite-agent meta-data exists "foo"`

type MetaDataExistsConfig struct {
	Key string `cli:"arg:0" label:"meta-data key" validate:"required"`
	Job string `cli:"job" validate:"required"`

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

var MetaDataExistsCommand = cli.Command{
	Name:        "exists",
	Usage:       "Check to see if the meta data key exists for a build",
	Description: MetaDataExistsHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "Which job's build should the meta-data be checked for",
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
		ExperimentsFlag,
		ProfileFlag,
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := MetaDataExistsConfig{}

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

		// Find the meta data value
		var err error
		var exists *api.MetaDataExists
		var resp *api.Response
		err = retry.Do(func(s *retry.Stats) error {
			exists, resp, err = client.ExistsMetaData(cfg.Job, cfg.Key)
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404) {
				s.Break()
			}
			if err != nil {
				l.Warn("%s (%s)", err, s)
			}

			return err
		}, &retry.Config{Maximum: 10, Interval: 5 * time.Second})
		if err != nil {
			l.Fatal("Failed to see if meta-data exists: %s", err)
		}

		// If the meta data didn't exist, exit with an error.
		if !exists.Exists {
			os.Exit(100)
		}
	},
}
