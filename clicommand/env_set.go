package clicommand

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/jobapi"
	"github.com/urfave/cli"
)

const envSetHelpDescription = `Usage:

    buildkite-agent env set [variable]

Description:

Sets environment variables in the current job execution environment.
Changes to the job environment variables only apply to subsequent phases of the job.
This command cannot unset Buildkite read-only variables.

To read the new values of variables from within the current phase, use ′env get′.

Examples:

Setting the variables ′LLAMA′ and ′ALPACA′:

    $ buildkite-agent env set LLAMA=Kuzco "ALPACA=Geronimo the Incredible"
    Added:
    + LLAMA
    Updated:
    ~ ALPACA

Setting the variables ′LLAMA′ and ′ALPACA′ using a JSON object supplied over standard input:

    $ echo '{"ALPACA":"Geronimo the Incredible","LLAMA":"Kuzco"}' | \
        buildkite-agent env set --input-format=json --output-format=quiet -`

type EnvSetConfig struct {
	GlobalConfig

	InputFormat  string `cli:"input-format"`
	OutputFormat string `cli:"output-format"`
}

var EnvSetCommand = cli.Command{
	Name:        "set",
	Usage:       "Sets variables in the job execution environment",
	Description: envSetHelpDescription,
	Flags: append(globalFlags(),
		cli.StringFlag{
			Name:   "input-format",
			Usage:  "Input format: plain or json",
			EnvVar: "BUILDKITE_AGENT_ENV_SET_INPUT_FORMAT",
			Value:  "plain",
		},
		cli.StringFlag{
			Name:   "output-format",
			Usage:  "Output format: quiet (no output), plain, json, or json-pretty",
			EnvVar: "BUILDKITE_AGENT_ENV_SET_OUTPUT_FORMAT",
			Value:  "plain",
		},
	),
	Action: envSetAction,
}

func envSetAction(c *cli.Context) error {
	ctx := context.Background()
	ctx, cfg, l, _, done := setupLoggerAndConfig[EnvSetConfig](ctx, c)
	defer done()

	client, err := jobapi.NewDefaultClient(ctx)
	if err != nil {
		return fmt.Errorf(envClientErrMessage, err)
	}

	req := &jobapi.EnvUpdateRequest{
		Env: make(map[string]string),
	}

	var parse func(string) error

	switch cfg.InputFormat {
	case "plain":
		parse = func(input string) error {
			e, v, ok := env.Split(input)
			if !ok {
				return fmt.Errorf("%q is not in key=value format", input)
			}
			req.Env[e] = v
			return nil
		}

	case "json":
		// Parse directly into the map
		parse = func(input string) error {
			return json.Unmarshal([]byte(input), &req.Env)
		}

	default:
		return fmt.Errorf("invalid input format %q", cfg.InputFormat)
	}

	// Inspect each arg, which could either be "-" for stdin, or "KEY=value"
	for _, arg := range c.Args() {
		if arg == "-" {
			// TODO: replace with c.App.Reader (or something like that) when we upgrade to urfave/cli v3
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

	resp, err := client.EnvUpdate(ctx, req)
	if err != nil {
		l.Error("Couldn't update the job executor environment: %v", err)
	}

	switch cfg.OutputFormat {
	case "quiet":
		return nil

	case "plain":
		if len(resp.Added) > 0 {
			fmt.Fprintln(c.App.Writer, "Added:")
			for _, a := range resp.Added {
				fmt.Fprintf(c.App.Writer, "+ %s\n", a)
			}
		}
		if len(resp.Updated) > 0 {
			fmt.Fprintln(c.App.Writer, "Updated:")
			for _, u := range resp.Updated {
				fmt.Fprintf(c.App.Writer, "~ %s\n", u)
			}
		}
		if len(resp.Added) == 0 && len(resp.Updated) == 0 {
			fmt.Fprintln(c.App.Writer, "No variables added or updated.")
		}

	case "json", "json-pretty":
		enc := json.NewEncoder(c.App.Writer)
		if c.String("output-format") == "json-pretty" {
			enc.SetIndent("", "  ")
		}
		if err := enc.Encode(resp); err != nil {
			return fmt.Errorf("error marshalling JSON: %w", err)
		}

	default:
		return fmt.Errorf("invalid output format %q", cfg.OutputFormat)
	}

	return nil
}
