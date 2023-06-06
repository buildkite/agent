package clicommand

import (
	"context"
	"fmt"
	"os"

	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/lock"
	"github.com/urfave/cli"
)

const lockReleaseHelpDescription = `Usage:

   buildkite-agent lock release [key]

Description:
   Releases the lock for the given key. This should only be called by the
   process that acquired the lock.

Examples:

   $ token=$(buildkite-agent lock acquire llama)
   $ critical_section()
   $ buildkite-agent lock release llama "${token}"

`

type LockReleaseConfig struct {
	// Common config options
	LockScope   string `cli:"lock-scope"`
	SocketsPath string `cli:"sockets-path" normalize:"filepath"`
}

var LockReleaseCommand = cli.Command{
	Name:        "release",
	Usage:       "Releases a previously-acquired lock",
	Description: lockReleaseHelpDescription,
	Flags:       lockCommonFlags,
	Action:      lockReleaseAction,
}

func lockReleaseAction(c *cli.Context) error {
	if c.NArg() != 2 {
		fmt.Fprint(c.App.ErrWriter, lockReleaseHelpDescription)
		os.Exit(1)
	}
	key, token := c.Args()[0], c.Args()[1]

	// Load the configuration
	cfg := LockReleaseConfig{}
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

	if err := cli.Unlock(ctx, key, token); err != nil {
		fmt.Fprintf(c.App.ErrWriter, "Could not release lock: %v\n", err)
		os.Exit(1)
	}

	return nil
}
