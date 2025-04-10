package clicommand

import (
	"context"
	"fmt"

	"github.com/buildkite/agent/v3/lock"
	"github.com/urfave/cli"
)

const lockReleaseHelpDescription = `Usage:

    buildkite-agent lock release [key] [token]

Description:

Releases the lock for the given key. This should only be called by the
process that acquired the lock. To help prevent different processes unlocking
each other unintentionally, the output from ′lock acquire′ is required as the
second argument, namely, the ′token′ in the Usage section above.

Note that this subcommand is only available when an agent has been started
with the ′agent-api′ experiment enabled.

Examples:

    #!/usr/bin/env bash
    token=$(buildkite-agent lock acquire llama)
    # your critical section here...
    buildkite-agent lock release llama "${token}"`

type LockReleaseConfig struct {
	GlobalConfig
	LockCommonConfig
}

var LockReleaseCommand = cli.Command{
	Name:        "release",
	Usage:       "Releases a previously-acquired lock",
	Description: lockReleaseHelpDescription,
	Flags:       lockCommonFlags(),
	Action:      lockReleaseAction,
}

func lockReleaseAction(c *cli.Context) error {
	if c.NArg() != 2 {
		fmt.Fprint(c.App.ErrWriter, lockReleaseHelpDescription)
		return &SilentExitError{code: 1}
	}
	key, token := c.Args()[0], c.Args()[1]

	ctx, cfg, _, _, done := setupLoggerAndConfig[LockReleaseConfig](context.Background(), c)
	defer done()

	if cfg.LockScope != "machine" {
		return fmt.Errorf("only 'machine' scope for locks is supported in this version.")
	}

	client, err := lock.NewClient(ctx, cfg.SocketsPath)
	if err != nil {
		return fmt.Errorf(lockClientErrMessage, err)
	}

	if err := client.Unlock(ctx, key, token); err != nil {
		return fmt.Errorf("could not release lock: %w", err)
	}

	return nil
}
