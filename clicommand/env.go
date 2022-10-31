package clicommand

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/buildkite/agent/v3/env"
	"github.com/urfave/cli"
)

const envDescription = `Usage:
  buildkite-agent env [options]

Description:
   Prints out the environment of the current process as a JSON object, easily
   parsable by other programs. Used when executing hooks to discover changes
   that hooks make to the environment.

Example:
   $ buildkite-agent env

   Prints the environment passed into the process
`

var EnvCommand = cli.Command{
	Name:        "env",
	Usage:       "Prints out the environment of the current process as a JSON object",
	Description: envDescription,
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:   "pretty",
			Usage:  "Pretty print the JSON output",
			EnvVar: "BUILDKITE_AGENT_ENV_PRETTY",
		},
	},
	Action: func(c *cli.Context) error {
		envn := os.Environ()
		envMap := make(map[string]string, len(envn))

		for _, e := range envn {
			k, v, ok := env.Split(e)
			if !ok {
				fmt.Fprintf(c.App.ErrWriter, "Invalid environment variable from os.Environ: %q\n", e)
				os.Exit(2)
			}
			envMap[k] = v
		}

		enc := json.NewEncoder(c.App.Writer)
		if c.Bool("pretty") {
			enc.SetIndent("", "  ")
		}

		if err := enc.Encode(envMap); err != nil {
			fmt.Fprintf(c.App.ErrWriter, "Error marshalling JSON: %v\n", err)
			os.Exit(1)
		}
		return nil
	},
}
