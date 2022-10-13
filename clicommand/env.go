package clicommand

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli"
)

const envDescription = `Usage:
  buildkite-agent env [options]

Description:
   Prints out the environment of the current process as a JSON object, easily parsable by other programs. Used when
   executing hooks to discover changes that hooks make to the environment.

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
		env := os.Environ()
		envMap := make(map[string]string, len(env))

		for _, e := range env {
			k, v, _ := strings.Cut(e, "=")
			envMap[k] = v
		}

		var (
			envJSON []byte
			err     error
		)

		if c.Bool("pretty") {
			envJSON, err = json.MarshalIndent(envMap, "", "  ")
		} else {
			envJSON, err = json.Marshal(envMap)
		}

		if err != nil {
			fmt.Printf("Error marshalling JSON: %v\n", err)
			os.Exit(1)
		}

		fmt.Println(string(envJSON))

		return nil
	},
}
