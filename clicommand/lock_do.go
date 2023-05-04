package clicommand

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/internal/agentapi"
	"github.com/urfave/cli"
)

const lockDoHelpDescription = `Usage:

   buildkite-agent lock do [key]

Description:
   Begins a do-once lock. Do-once can be used by multiple processes to 
   wait for completion of some shared work, where only one process should do
   the work. 
   
   ′lock do′ will do one of two things:
   
   - Print 'do'. The calling process should proceed to do the work and then
     call ′lock done′.
   - Wait until the work is marked as done (with ′lock done′) and print 'done'.
   
   If ′lock do′ prints 'done' immediately, the work was already done.

Examples:

   #!/bin/bash
   if [ $(buildkite-agent lock do llama) = 'do' ] ; then
      setup_code()
      buildkite-agent lock done llama
   fi

`

type LockDoConfig struct {
	SocketsPath string `cli:"sockets-path" normalize:"filepath"`
}

var LockDoCommand = cli.Command{
	Name:        "do",
	Usage:       "Begins a do-once lock",
	Description: lockDoHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "sockets-path",
			Value:  defaultSocketsPath(),
			Usage:  "Directory where the agent will place sockets",
			EnvVar: "BUILDKITE_SOCKETS_PATH",
		},
	},
	Action: lockDoAction,
}

func lockDoAction(c *cli.Context) error {
	if c.NArg() != 1 {
		fmt.Fprint(c.App.ErrWriter, lockDoHelpDescription)
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

	for {
		state, err := cli.Get(ctx, key)
		if err != nil {
			fmt.Fprintf(c.App.ErrWriter, "Error performing get: %v\n", err)
			os.Exit(1)
		}

		switch state {
		case "":
			// Try to acquire the lock by changing to state 1
			_, done, err := cli.CompareAndSwap(ctx, key, "", "doing")
			if err != nil {
				fmt.Fprintf(c.App.ErrWriter, "Error performing compare-and-swap: %v\n", err)
				os.Exit(1)
			}
			if done {
				// Lock acquired, exit 0.
				fmt.Fprintln(c.App.Writer, "do")
				return nil
			}
			// Lock not acquired (perhaps something else acquired it).
			// Go through the loop again.

		case "doing":
			// Work in progress - wait until state 2.
			if err := sleep(ctx, 100*time.Millisecond); err != nil {
				fmt.Fprintf(c.App.ErrWriter, "Exceeded deadline or context cancelled: %v\n", err)
				os.Exit(1)
			}

		case "done":
			// Work completed!
			fmt.Fprintln(c.App.Writer, "done")
			return nil

		default:
			// Invalid state.
			fmt.Fprintln(c.App.ErrWriter, "Lock in invalid state for do-once - investigate with 'lock get'")
			os.Exit(1)
		}

	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
