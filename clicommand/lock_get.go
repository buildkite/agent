package clicommand

import (
	"context"
	"fmt"
	"os"

	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/lock"
	"github.com/urfave/cli"
)

const lockGetHelpDescription = `Usage:

   buildkite-agent lock get [key]

Description:
   Retrieves the value of a lock key. Any key not in use returns an empty 
   string.
   
   Note that this subcommand is only available when an agent has been started
   with the ′agent-api′ experiment enabled.
   
   ′lock get′ is generally only useful for inspecting lock state, as the value
   can change concurrently. To acquire or release a lock, use ′lock acquire′ and
   ′lock release′.

Examples:

   $ buildkite-agent lock get llama
   Kuzco

`

type LockGetConfig struct {
	// Common config options
	LockScope   string `cli:"lock-scope"`
	SocketsPath string `cli:"sockets-path" normalize:"filepath"`
}

var LockGetCommand = cli.Command{
	Name:        "get",
	Usage:       "Gets a lock value from the agent leader",
	Description: lockGetHelpDescription,
	Flags:       lockCommonFlags,
	Action:      lockGetAction,
}

func lockGetAction(c *cli.Context) error {
	if c.NArg() != 1 {
		fmt.Fprint(c.App.ErrWriter, lockGetHelpDescription)
		os.Exit(1)
	}
	key := c.Args()[0]

	// Load the configuration
	cfg := LockGetConfig{}
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

	cli, err := lock.NewClient(ctx, cfg.SocketsPath)
	if err != nil {
		fmt.Fprintf(c.App.ErrWriter, lockClientErrMessage, err)
		os.Exit(1)
	}

	v, err := cli.Get(ctx, key)
	if err != nil {
		fmt.Fprintf(c.App.ErrWriter, "Couldn't get lock state: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintln(c.App.Writer, v)
	return nil
}
