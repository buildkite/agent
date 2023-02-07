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
   Un-sets environment variables in the current job execution environment. 

   Note that this subcommand is only available from within the job executor with
   the ′job-api′ experiment enabled.

   Note that changes to the job environment variables only apply to subsequent
   phases of the job. To read the new values of variables from within the
   current phase, use ′env get′.

   Note that Buildkite read-only variables cannot be un-set.

Example (un-sets the variables ′LLAMA′ and ′ALPACA′):

    $ buildkite-agent env unset LLAMA ALPACA
    Un-set:
    - ALPACA
    - LLAMA
	
Example (Un-sets the variables ′LLAMA′ and ′ALPACA′ with a JSON list supplied
over standard input):
    
    $ echo '["LLAMA","ALPACA"]' | buildkite-agent env unset --input-format=json --output-format=quiet -
`

type EnvUnsetConfig struct{}

var EnvUnsetCommand = cli.Command{
	Name:        "unset",
	Usage:       "Un-sets variables from the job execution environment",
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
	},
	Action: envUnsetAction,
}

func envUnsetAction(c *cli.Context) error {
	cli, err := jobapi.NewDefaultClient()
	if err != nil {
		fmt.Fprintf(c.App.ErrWriter, envClientErrMessage, err)
		os.Exit(1)
	}

	var del []string

	var parse func(string) error

	switch c.String("input-format") {
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

	unset, err := cli.EnvDelete(context.Background(), del)
	if err != nil {
		fmt.Fprintf(c.App.ErrWriter, "Couldn't un-set the job executor environment variables: %v\n", err)
	}

	switch c.String("output-format") {
	case "quiet":
		return nil

	case "plain":
		if len(unset) > 0 {
			fmt.Fprintln(c.App.Writer, "Un-set:")
			for _, d := range unset {
				fmt.Fprintf(c.App.Writer, "- %s\n", d)
			}
		} else {
			fmt.Fprintln(c.App.Writer, "No variables un-set.")
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
