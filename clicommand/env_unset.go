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

Note that this subcommand is only available from within the job executor with the job-api experiment enabled.

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
	InputFormat  string `cli:"input-format"`
	OutputFormat string `cli:"output-format"`

	// Global flags
	Debug       bool     `cli:"debug"`
	LogLevel    string   `cli:"log-level"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`
}

var EnvUnsetCommand = cli.Command{
	Name:        "unset",
	Usage:       "Unsets variables from the job execution environment",
	Description: envUnsetHelpDescription,
	Flags: []cli.Flag{
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

		// Global flags
		NoColorFlag,
		DebugFlag,
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
	},
	Action: envUnsetAction,
}

func envUnsetAction(c *cli.Context) error {
	ctx := context.Background()
	cfg, l, _, done := setupLoggerAndConfig[EnvUnsetConfig](c)
	defer done()

	client, err := jobapi.NewDefaultClient(ctx)
	if err != nil {
		l.Fatal(envClientErrMessage, err)
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
		fmt.Fprintf(c.App.ErrWriter, "Invalid input format %q\n", c.String("input-format"))
	}

	// Inspect each arg, which could either be "-" for stdin, or "KEY"
	for _, arg := range c.Args() {
		if arg == "-" {
			// Parse standard input
			sc := bufio.NewScanner(os.Stdin)
			line := 1
			for sc.Scan() {
				if err := parse(sc.Text()); err != nil {
					fmt.Fprintf(c.App.ErrWriter, "Couldn't parse input line %d: %v\n", line, err)
					os.Exit(1)
				}
				line++
			}
			if err := sc.Err(); err != nil {
				fmt.Fprintf(c.App.ErrWriter, "Couldn't scan the input buffer: %v\n", err)
				os.Exit(1)
			}
			continue
		}
		// Parse args directly
		if err := parse(arg); err != nil {
			fmt.Fprintf(c.App.ErrWriter, "Couldn't parse the command-line argument %q: %v\n", arg, err)
			os.Exit(1)
		}
	}

	unset, err := client.EnvDelete(ctx, del)
	if err != nil {
		fmt.Fprintf(c.App.ErrWriter, "Couldn't unset the job executor environment variables: %v\n", err)
	}

	switch cfg.OutputFormat {
	case "quiet":
		return nil

	case "plain":
		if len(unset) > 0 {
			fmt.Fprintln(c.App.Writer, "Unset:")
			for _, d := range unset {
				fmt.Fprintf(c.App.Writer, "- %s\n", d)
			}
		} else {
			fmt.Fprintln(c.App.Writer, "No variables unset.")
		}

	case "json", "json-pretty":
		enc := json.NewEncoder(c.App.Writer)
		if c.String("output-format") == "json-pretty" {
			enc.SetIndent("", "  ")
		}
		if err := enc.Encode(unset); err != nil {
			fmt.Fprintf(c.App.ErrWriter, "Error marshalling JSON: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(c.App.ErrWriter, "Invalid output format %q\n", c.String("output-format"))
	}

	return nil
}
