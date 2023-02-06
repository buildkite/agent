package clicommand

import (
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
	},
	Action: func(c *cli.Context) error {
		envn := os.Environ()
		envMap := make(map[string]string, len(envn))

		for _, e := range envn {
			if k, v, ok := env.Split(e); ok {
				envMap[k] = v
			}
		}

		enc := json.NewEncoder(c.App.Writer)
		if c.String("format") == "json-pretty" {
			enc.SetIndent("", "  ")
		}

		if err := enc.Encode(envMap); err != nil {
			fmt.Fprintf(c.App.ErrWriter, "Error marshalling JSON: %v\n", err)
			os.Exit(1)
		}
		return nil
	},
}
