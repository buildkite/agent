package clicommand

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/jobapi"
	"github.com/urfave/cli"
)

const envClientErrMessage = `Could not create Job API client: %v
This command can only be used from hooks or plugins running under a job executor
where the "job-api" experiment is enabled.
`

const envGetHelpDescription = `Usage:

   buildkite-agent env get [variables]

Description:
   Retrieves environment variables and their current values from the current job
   execution environment.

   Note that this subcommand is only available from within the job executor with
   the ′job-api′ experiment enabled.

   Changes to the job environment only apply to the environments of subsequent
   phases of the job. However, ′env get′ can be used to inspect the changes made
   with ′env set′ and ′env unset′.

Example (gets all variables in key=value format):

   $ buildkite-agent env get
   ALPACA=Geronimo the Incredible
   BUILDKITE=true
   LLAMA=Kuzco
   ...

Example (gets the value of one variable):

   $ buildkite-agent env get LLAMA
   Kuzco

Example (gets multiple specific variables):

   $ buildkite-agent env get LLAMA ALPACA
   ALPACA=Geronimo the Incredible
   LLAMA=Kuzco

Example (gets variables as a JSON object):

   $ buildkite-agent env get --format=json-pretty
   {
     "ALPACA": "Geronimo the Incredible",
     "BUILDKITE": "true",
     "LLAMA": "Kuzco",
     ...
   }
`

type EnvGetConfig struct{}

var EnvGetCommand = cli.Command{
	Name:        "get",
	Usage:       "Gets variables from the job execution environment",
	Description: envGetHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "format",
			Usage:  "Output format: plain, json, or json-pretty",
			EnvVar: "BUILDKITE_AGENT_ENV_GET_FORMAT",
			Value:  "plain",
		},
	},
	Action: envGetAction,
}

func envGetAction(c *cli.Context) error {
	client, err := jobapi.NewDefaultClient()
	if err != nil {
		fmt.Fprintf(c.App.ErrWriter, envClientErrMessage, err)
		os.Exit(1)
	}

	envMap, err := client.EnvGet(context.Background())
	if err != nil {
		fmt.Fprintf(c.App.ErrWriter, "Couldn't fetch the job executor environment variables: %v\n", err)
		os.Exit(1)
	}

	notFound := false

	// Filter envMap by any remaining args.
	if len(c.Args()) > 0 {
		em := make(map[string]string)
		for _, arg := range c.Args() {
			v, ok := envMap[arg]
			if !ok {
				notFound = true
				fmt.Fprintf(c.App.ErrWriter, "%q is not set\n", arg)
				continue
			}
			em[arg] = v
		}
		envMap = em
	}

	switch c.String("format") {
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
			fmt.Fprintf(c.App.ErrWriter, "Error marshalling JSON: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(c.App.ErrWriter, "Invalid output format %q\n", c.String("format"))
	}

	if notFound {
		os.Exit(1)
	}
	return nil
}
