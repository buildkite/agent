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
	// Common config options
	LockScope   string `cli:"lock-scope"`
	SocketsPath string `cli:"sockets-path" normalize:"filepath"`

	LockWaitTimeout time.Duration `cli:"lock-wait-timeout"`
}

var LockDoCommand = cli.Command{
	Name:        "do",
	Usage:       "Begins a do-once lock",
	Description: lockDoHelpDescription,
	Flags: append(
		lockCommonFlags,
		cli.DurationFlag{
			Name:   "lock-wait-timeout",
			Usage:  "If specified, sets a maximum duration to wait for a lock before giving up",
			EnvVar: "BUILDKITE_LOCK_WAIT_TIMEOUT",
		},
	),
	Action: lockDoAction,
}

func lockDoAction(c *cli.Context) error {
	if c.NArg() != 1 {
		fmt.Fprint(c.App.ErrWriter, lockDoHelpDescription)
		os.Exit(1)
	}
	key := c.Args()[0]

	// Load the configuration
	cfg := LockDoConfig{}
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

	if cfg.LockScope != "machine" {
		fmt.Fprintln(c.App.Writer, "Only 'machine' scope for locks is supported in this version.")
		os.Exit(1)
	}

	ctx := context.Background()
	if cfg.LockWaitTimeout != 0 {
		cctx, canc := context.WithTimeout(ctx, cfg.LockWaitTimeout)
		defer canc()
		ctx = cctx
	}

	cli, err := agentapi.NewClient(ctx, agentapi.LeaderPath(cfg.SocketsPath))
	if err != nil {
		fmt.Fprintf(c.App.ErrWriter, lockClientErrMessage, err)
		os.Exit(1)
	}

	for {
		state, err := cli.LockGet(ctx, key)
		if err != nil {
			fmt.Fprintf(c.App.ErrWriter, "Error performing get: %v\n", err)
			os.Exit(1)
		}

		switch state {
		case "":
			// Try to acquire the lock by changing to state 1
			_, done, err := cli.LockCompareAndSwap(ctx, key, "", "doing")
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
			fmt.Fprintf(c.App.ErrWriter, "Lock in invalid state %q for do-once\n", state)
			os.Exit(1)
		}
	}
}
