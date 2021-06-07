package clicommand

import (
	"io/ioutil"
	"os"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/retry"
	"github.com/urfave/cli"
)

var MetaDataSetHelpDescription = `Usage:

   buildkite-agent meta-data set <key> [value] [options...]

Description:

   Set arbitrary data on a build using a basic key/value store.

   You can supply the value as an argument to the command, or pipe in a file or
   script output.

Example:

   $ buildkite-agent meta-data set "foo" "bar"
   $ buildkite-agent meta-data set "foo" < ./tmp/meta-data-value
   $ ./script/meta-data-generator | buildkite-agent meta-data set "foo"`

type MetaDataSetConfig struct {
	Key   string `cli:"arg:0" label:"meta-data key" validate:"required"`
	Value string `cli:"arg:1" label:"meta-data value"`
	Job   string `cli:"job" validate:"required"`

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

var MetaDataSetCommand = cli.Command{
	Name:        "set",
	Usage:       "Set data on a build",
	Description: MetaDataSetHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "Which job's build should the meta-data be set on",
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
		cfg := MetaDataSetConfig{}

		l := CreateLogger(&cfg)

		// Load the configuration
		if err := cliconfig.Load(c, l, &cfg); err != nil {
			l.Fatal("%s", err)
		}

		// Setup any global configuration options
		done := HandleGlobalFlags(l, cfg)
		defer done()

		// Read the value from STDIN if argument omitted entirely
		if len(c.Args()) < 2 {
			l.Info("Reading meta-data value from STDIN")

			input, err := ioutil.ReadAll(os.Stdin)
			if err != nil {
				l.Fatal("Failed to read from STDIN: %s", err)
			}
			cfg.Value = string(input)
		}

		// Create the API client
		client := api.NewClient(l, loadAPIClientConfig(cfg, `AgentAccessToken`))

		// Create the meta data to set
		metaData := &api.MetaData{
			Key:   cfg.Key,
			Value: cfg.Value,
		}

		// Set the meta data
		err := retry.Do(func(s *retry.Stats) error {
			resp, err := client.SetMetaData(cfg.Job, metaData)
			if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 404) {
				s.Break()
			}
			if err != nil {
				l.Warn("%s (%s)", err, s)
			}

			return err
		}, &retry.Config{Maximum: 10, Interval: 5 * time.Second})
		if err != nil {
			l.Fatal("Failed to set meta-data: %s", err)
		}
	},
}
