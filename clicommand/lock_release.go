package clicommand

import (
	"context"
	"fmt"
	"os"

	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/internal/agentapi"
	"github.com/urfave/cli"
)

const lockReleaseHelpDescription = `Usage:

   buildkite-agent lock release [key]

Description:
   Releases the lock for the given key. This should only be called by the
   process that acquired the lock.

Examples:

   $ buildkite-agent lock acquire llama
   $ critical_section()
   $ buildkite-agent lock release llama

`

type LockReleaseConfig struct {
	SocketsPath string `cli:"sockets-path" normalize:"filepath"`
}

var LockReleaseCommand = cli.Command{
	Name:        "release",
	Usage:       "Releases a previously-acquired lock",
	Description: lockReleaseHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "sockets-path",
			Value:  defaultSocketsPath(),
			Usage:  "Directory where the agent will place sockets",
			EnvVar: "BUILDKITE_SOCKETS_PATH",
		},
	},
	Action: lockReleaseAction,
}

func lockReleaseAction(c *cli.Context) error {
	if c.NArg() != 1 {
		fmt.Fprint(c.App.ErrWriter, lockReleaseHelpDescription)
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

	val, done, err := cli.CompareAndSwap(ctx, key, "acquired", "")
	if err != nil {
		fmt.Fprintf(c.App.ErrWriter, "Error performing compare-and-swap: %v\n", err)
		os.Exit(1)
	}

	if !done {
		fmt.Fprintf(c.App.ErrWriter, "Lock in invalid state %q to release - investigate with 'lock get'\n", val)
		os.Exit(1)
	}
	return nil
}
