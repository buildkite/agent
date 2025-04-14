package clicommand

import (
	"context"
	"errors"
	"fmt"

	"github.com/buildkite/agent/v3/lock"
	"github.com/urfave/cli"
)

const lockDoneHelpDescription = `Usage:

    buildkite-agent lock done [key]

Description:

Completes a do-once lock. This should only be used by the process performing
the work.

Note that this subcommand is only available when an agent has been started
with the ′agent-api′ experiment enabled.

Examples:

    #!/usr/bin/env bash
    if [[ $(buildkite-agent lock do llama) == 'do' ]]; then
      # your critical section here...
      buildkite-agent lock done llama
    fi`

type LockDoneConfig struct {
	GlobalConfig
	LockCommonConfig
}

var LockDoneCommand = cli.Command{
	Name:        "done",
	Usage:       "Completes a do-once lock",
	Description: lockDoneHelpDescription,
	Flags:       lockCommonFlags(),
	Action:      lockDoneAction,
}

func lockDoneAction(c *cli.Context) error {
	if c.NArg() != 1 {
		fmt.Fprint(c.App.ErrWriter, lockDoneHelpDescription)
		return &SilentExitError{code: 1}
	}
	key := c.Args()[0]

	ctx, cfg, _, _, done := setupLoggerAndConfig[LockDoneConfig](context.Background(), c)
	defer done()

	if cfg.LockScope != "machine" {
		return errors.New("only 'machine' scope for locks is supported in this version.")
	}

	client, err := lock.NewClient(ctx, cfg.SocketsPath)
	if err != nil {
		return fmt.Errorf(lockClientErrMessage, err)
	}

	if err := client.DoOnceEnd(ctx, key); err != nil {
		return fmt.Errorf("couldn't complete do-once lock: %w", err)
	}

	return nil
}
