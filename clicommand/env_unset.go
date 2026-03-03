package clicommand

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/buildkite/agent/v3/jobapi"
	"github.com/urfave/cli"
)

const envUnsetHelpDescription = `Usage:

    buildkite-agent env unset [variables]

Description:

Unsets environment variables in the current job execution environment.
Changes to the job environment variables only apply to subsequent phases of the job.
This command cannot unset Buildkite read-only variables.

To read the new values of variables from within the current phase, use ′env get′.

Examples:

Unsetting the variables ′LLAMA′ and ′ALPACA′:

    $ buildkite-agent env unset LLAMA ALPACA
    Unset:
    - ALPACA
    - LLAMA

Unsetting the variables ′LLAMA′ and ′ALPACA′ with a JSON list supplied over standard input

    $ echo '["LLAMA","ALPACA"]' | \
        buildkite-agent env unset --input-format=json --output-format=quiet -`

type EnvUnsetConfig struct {
	GlobalConfig

	InputFormat  string `cli:"input-format"`
	OutputFormat string `cli:"output-format"`
}

var EnvUnsetCommand = cli.Command{
	Name:        "unset",
	Usage:       "Unsets variables from the job execution environment",
	Description: envUnsetHelpDescription,
	Flags: append(globalFlags(),
		cli.StringFlag{
			Name:   "input-format",
			Usage:  "Input format: plain or json",
			EnvVar: "BUILDKITE_AGENT_ENV_UNSET_INPUT_FORMAT",
			Value:  "plain",
		},
		cli.StringFlag{
			Name:   "output-format",
			Usage:  "Output format: quiet (no output), plain, json, or json-pretty",
			EnvVar: "BUILDKITE_AGENT_ENV_UNSET_OUTPUT_FORMAT",
			Value:  "plain",
		},
	),
	Action: envUnsetAction,
}

func envUnsetAction(c *cli.Context) error {
	ctx := context.Background()
	ctx, cfg, l, _, done := setupLoggerAndConfig[EnvUnsetConfig](ctx, c)
	defer done()

	client, err := jobapi.NewDefaultClient(ctx)
	if err != nil {
		return fmt.Errorf(envClientErrMessage, err)
	}

	var del []string

	var parse func(string) error

	switch cfg.InputFormat {
	case "plain":
		parse = func(input string) error {
			del = append(del, input)
			return nil
		}

	case "json":
		parse = func(input string) error {
			return json.Unmarshal([]byte(input), &del)
		}

	default:
		fmt.Fprintf(c.App.ErrWriter, "Invalid input format %q\n", c.String("input-format")) //nolint:errcheck // CLI output; errors are non-actionable
	}

	// Inspect each arg, which could either be "-" for stdin, or "KEY"
	for _, arg := range c.Args() {
		if arg == "-" {
			// Parse standard input
			sc := bufio.NewScanner(os.Stdin)
			line := 1
			for sc.Scan() {
				if err := parse(sc.Text()); err != nil {
					return fmt.Errorf("couldn't parse input line %d: %w", line, err)
				}
				line++
			}
			if err := sc.Err(); err != nil {
				return fmt.Errorf("couldn't scan the input buffer: %w", err)
			}
			continue
		}
		// Parse args directly
		if err := parse(arg); err != nil {
			return fmt.Errorf("couldn't parse the command-line argument %q: %w", arg, err)
		}
	}

	unset, err := client.EnvDelete(ctx, del)
	if err != nil {
		l.Error("couldn't unset the job executor environment variables: %v", err)
	}

	switch cfg.OutputFormat {
	case "quiet":
		return nil

	case "plain":
		if len(unset) > 0 {
			fmt.Fprintln(c.App.Writer, "Unset:") //nolint:errcheck // CLI output; errors are non-actionable
			for _, d := range unset {
				fmt.Fprintf(c.App.Writer, "- %s\n", d) //nolint:errcheck // CLI output; errors are non-actionable
			}
			} else {
				fmt.Fprintln(c.App.Writer, "No variables unset.") //nolint:errcheck // CLI output; errors are non-actionable
		}

	case "json", "json-pretty":
		enc := json.NewEncoder(c.App.Writer)
		if c.String("output-format") == "json-pretty" {
			enc.SetIndent("", "  ")
		}
		if err := enc.Encode(unset); err != nil {
			return fmt.Errorf("error marshalling JSON: %w", err)
		}

	default:
		return fmt.Errorf("invalid output format %q", cfg.OutputFormat)
	}

	return nil
}
