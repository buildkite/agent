package clicommand

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/buildkite/agent/v3/env"
	"github.com/urfave/cli"
)

const envDumpHelpDescription = `Usage:

    buildkite-agent env dump [options]

Description:

Prints out the environment of the current process as a JSON object, easily
parsable by other programs. Used when executing hooks to discover changes
that hooks make to the environment.

Example:

    $ buildkite-agent env dump --format json-pretty`

type EnvDumpConfig struct {
	Format string `cli:"format"`

	// Global flags
	Debug       bool     `cli:"debug"`
	LogLevel    string   `cli:"log-level"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`
}

var EnvDumpCommand = cli.Command{
	Name:        "dump",
	Usage:       "Print the environment of the current process as a JSON object",
	Description: envDumpHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "format",
			Usage:  "Output format; json or json-pretty",
			EnvVar: "BUILDKITE_AGENT_ENV_DUMP_FORMAT",
			Value:  "json",
		},

		// Global flags
		NoColorFlag,
		DebugFlag,
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
	},
	Action: func(c *cli.Context) error {
		_, cfg, _, _, done := setupLoggerAndConfig[EnvDumpConfig](context.Background(), c)
		defer done()

		envn := os.Environ()
		envMap := make(map[string]string, len(envn))

		for _, e := range envn {
			if k, v, ok := env.Split(e); ok {
				envMap[k] = v
			}
		}

		enc := json.NewEncoder(c.App.Writer)
		if cfg.Format == "json-pretty" {
			enc.SetIndent("", "  ")
		}

		if err := enc.Encode(envMap); err != nil {
			return fmt.Errorf("error marshalling JSON: %w", err)
		}

		return nil
	},
}
