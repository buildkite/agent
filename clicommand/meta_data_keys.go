package clicommand

import (
	"fmt"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/retry"
	"github.com/urfave/cli"
)

var MetaDataKeysHelpDescription = `Usage:

   buildkite-agent meta-data keys [options...]

Description:

   Lists all meta-data keys that have been previously set, delimited by a newline
   and terminated with a trailing newline.

Example:

   $ buildkite-agent meta-data keys`

type MetaDataKeysConfig struct {
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

var MetaDataKeysCommand = cli.Command{
	Name:        "keys",
	Usage:       "Lists all meta-data keys that have been previously set",
	Description: MetaDataKeysHelpDescription,
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
		cfg := MetaDataKeysConfig{}

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

		// Find the meta data keys
		var err error
		var keys []string
		var resp *api.Response
		err = retry.Do(func(s *retry.Stats) error {
			keys, resp, err = client.MetaDataKeys(cfg.Job)
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404) {
				s.Break()
			}
			if err != nil {
				l.Warn("%s (%s)", err, s)
			}

			return err
		}, &retry.Config{Maximum: 10, Interval: 5 * time.Second})
		if err != nil {
			l.Fatal("Failed to find meta-data keys: %s", err)
		}

		for _, key := range keys {
			fmt.Printf("%s\n", key)
		}
	},
}
