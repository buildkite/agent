package clicommand

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/buildkite/agent/v4/env"
	"github.com/urfave/cli/v3"
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
	GlobalConfig

	Format string `cli:"format"`
}

var EnvDumpCommand = &cli.Command{
	Name:        "dump",
	Usage:       "Print the environment of the current process as a JSON object",
	Description: envDumpHelpDescription,
	Flags: append(globalFlags(),
		&cli.StringFlag{
			Name:    "format",
			Usage:   "Output format; json or json-pretty",
			Sources: cli.EnvVars("BUILDKITE_AGENT_ENV_DUMP_FORMAT"),
			Value:   "json",
		},
	),
	Action: func(ctx context.Context, c *cli.Command) error {
		_, cfg, _, _, done := setupLoggerAndConfig[EnvDumpConfig](ctx, c)
		defer done()

		envn := os.Environ()
		envMap := make(map[string]string, len(envn))

		for _, e := range envn {
			if k, v, ok := env.Split(e); ok {
				envMap[k] = v
			}
		}

		enc := json.NewEncoder(c.Writer)
		enc.SetEscapeHTML(false) // HTML escapes may interfere with secret redaction
		if cfg.Format == "json-pretty" {
			enc.SetIndent("", "  ")
		}

		if err := enc.Encode(envMap); err != nil {
			return fmt.Errorf("error marshalling JSON: %w", err)
		}

		return nil
	},
}
