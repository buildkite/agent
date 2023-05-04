package clicommand

import (
	"context"
	"fmt"
	"os"

	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/internal/agentapi"
	"github.com/urfave/cli"
)

const lockDoneHelpDescription = `Usage:

   buildkite-agent lock release [key]

Description:
   Completes a do-once lock. This should only be used by the process performing
   the work.

Examples:

   #!/bin/bash
   if [ $(buildkite-agent lock do llama) = 'do' ] ; then
	  setup_code()
	  buildkite-agent lock done llama
   fi


`

type LockDoneConfig struct {
	SocketsPath string `cli:"sockets-path" normalize:"filepath"`
}

var LockDoneCommand = cli.Command{
	Name:        "done",
	Usage:       "Completes a do-once lock",
	Description: lockDoneHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "sockets-path",
			Value:  defaultSocketsPath(),
			Usage:  "Directory where the agent will place sockets",
			EnvVar: "BUILDKITE_SOCKETS_PATH",
		},
	},
	Action: lockDoneAction,
}

func lockDoneAction(c *cli.Context) error {
	if c.NArg() != 1 {
		fmt.Fprint(c.App.ErrWriter, lockDoneHelpDescription)
		os.Exit(1)
	}
	key := c.Args()[0]

	// Load the configuration
	cfg := LockAcquireConfig{}
	loader := cliconfig.Loader{
		CLI:                    c,
		Config:                 &cfg,
		DefaultConfigFilePaths: DefaultConfigFilePaths(),
	}
	warnings, err := loader.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %s\n", err)
		os.Exit(1)
	}
	for _, warning := range warnings {
		fmt.Fprintln(c.App.ErrWriter, warning)
	}

	ctx := context.Background()

	cli, err := agentapi.NewClient(agentapi.LeaderPath(cfg.SocketsPath))
	if err != nil {
		fmt.Fprintf(c.App.ErrWriter, lockClientErrMessage, err)
		os.Exit(1)
	}

	val, done, err := cli.CompareAndSwap(ctx, key, "doing", "done")
	if err != nil {
		fmt.Fprintf(c.App.ErrWriter, "Error performing compare-and-swap: %v\n", err)
		os.Exit(1)
	}

	if !done {
		fmt.Fprintf(c.App.ErrWriter, "Lock in invalid state %q to mark complete - investigate with 'lock get'\n", val)
		os.Exit(1)
	}
	return nil
}
