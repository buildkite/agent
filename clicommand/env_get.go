package clicommand

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/jobapi"
	"github.com/urfave/cli"
)

const envClientErrMessage = `Could not create Job API client: %w
This command can only be used from hooks or plugins running under a job executor
where the agent's job API is available (in version v3.64.0 and later of the Buildkite Agent).`

const envGetHelpDescription = `Usage:

    buildkite-agent env get [variables]

Description:

Retrieves environment variables and their current values from the current job
execution environment.

Changes to the job environment only apply to the environments of subsequent
phases of the job. However, ′env get′ can be used to inspect the changes made
with ′env set′ and ′env unset′.

Examples:

Getting all variables in key=value format:

    $ buildkite-agent env get
    ALPACA=Geronimo the Incredible
    BUILDKITE=true
    LLAMA=Kuzco
    ...

Getting the value of one variable:

    $ buildkite-agent env get LLAMA
    Kuzco

Getting multiple specific variables:

    $ buildkite-agent env get LLAMA ALPACA
    ALPACA=Geronimo the Incredible
    LLAMA=Kuzco

Getting variables as a JSON object:

    $ buildkite-agent env get --format=json-pretty
    {
      "ALPACA": "Geronimo the Incredible",
      "BUILDKITE": "true",
      "LLAMA": "Kuzco",
      ...
    }`

type EnvGetConfig struct {
	GlobalConfig

	Format string `cli:"format"`
}

var EnvGetCommand = cli.Command{
	Name:        "get",
	Usage:       "Gets variables from the job execution environment",
	Description: envGetHelpDescription,
	Flags: append(globalFlags(),
		cli.StringFlag{
			Name:   "format",
			Usage:  "Output format: plain, json, or json-pretty",
			EnvVar: "BUILDKITE_AGENT_ENV_GET_FORMAT",
			Value:  "plain",
		},
	),
	Action: envGetAction,
}

func envGetAction(c *cli.Context) error {
	ctx := context.Background()
	ctx, cfg, l, _, done := setupLoggerAndConfig[EnvGetConfig](ctx, c)
	defer done()

	client, err := jobapi.NewDefaultClient(ctx)
	if err != nil {
		return fmt.Errorf(envClientErrMessage, err)
	}

	envMap, err := client.EnvGet(ctx)
	if err != nil {
		return fmt.Errorf("couldn't fetch the job executor environment variables: %w", err)
	}

	notFound := false

	// Filter envMap by any remaining args.
	if len(c.Args()) > 0 {
		em := make(map[string]string)
		for _, arg := range c.Args() {
			v, ok := envMap[arg]
			if !ok {
				notFound = true
				l.Warn("%q is not set", arg)
				continue
			}
			em[arg] = v
		}
		envMap = em
	}

	switch cfg.Format {
	case "plain":
		if len(c.Args()) == 1 {
			// Just print the value.
			for _, v := range envMap {
				fmt.Fprintln(c.App.Writer, v)
			}
			break
		}

		// Print everything.
		for _, v := range env.FromMap(envMap).ToSlice() {
			fmt.Fprintln(c.App.Writer, v)
		}

	case "json", "json-pretty":
		enc := json.NewEncoder(c.App.Writer)
		if c.String("format") == "json-pretty" {
			enc.SetIndent("", "  ")
		}
		if err := enc.Encode(envMap); err != nil {
			return fmt.Errorf("error marshalling JSON: %w", err)
		}

	default:
		l.Error("Invalid output format %q\n", cfg.Format)
	}

	if notFound {
		return &SilentExitError{code: 1}
	}

	return nil
}
