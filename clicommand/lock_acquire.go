package clicommand

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/buildkite/agent/v4/lock"
	"github.com/urfave/cli/v3"
)

const lockAcquireHelpDescription = `Usage:

    buildkite-agent lock acquire [key]

Description:

Acquires the lock for the given key. ′lock acquire′ will wait (potentially
forever) until it can acquire the lock, if the lock is already held by
another process. If multiple processes are waiting for the same lock, there
is no ordering guarantee of which one will be given the lock next.

To prevent separate processes unlocking each other, the output from ′lock
acquire′ should be stored, and passed to ′lock release′.

Note that this subcommand is only available when an agent has been started
with the ′agent-api′ experiment enabled.

Examples:

    #!/usr/bin/env bash
    token=$(buildkite-agent lock acquire llama)
    # your critical section here...
    buildkite-agent lock release llama "${token}"`

type LockAcquireConfig struct {
	GlobalConfig
	LockCommonConfig

	LockWaitTimeout time.Duration `cli:"lock-wait-timeout"`
}

var LockAcquireCommand = &cli.Command{
	Name:        "acquire",
	Usage:       "Acquires a lock from the agent leader",
	Description: lockAcquireHelpDescription,
	Flags: append(lockCommonFlags(),
		&cli.DurationFlag{
			Name:    "lock-wait-timeout",
			Usage:   "Sets a maximum duration to wait for a lock before giving up",
			Sources: cli.EnvVars("BUILDKITE_LOCK_WAIT_TIMEOUT"),
		},
	),
	Action: lockAcquireAction,
}

func lockAcquireAction(ctx context.Context, c *cli.Command) error {
	if c.NArg() != 1 {
		_, _ = fmt.Fprint(c.ErrWriter, lockAcquireHelpDescription)
		return &SilentExitError{code: 1}
	}
	key := c.Args().Get(0)

	ctx, cfg, _, _, done := setupLoggerAndConfig[LockAcquireConfig](ctx, c)
	defer done()

	if cfg.LockScope != "machine" {
		return errors.New("only 'machine' scope for locks is supported in this version")
	}

	if cfg.LockWaitTimeout != 0 {
		cctx, canc := context.WithTimeout(ctx, cfg.LockWaitTimeout)
		defer canc()
		ctx = cctx
	}

	client, err := lock.NewClient(ctx, cfg.SocketsPath)
	if err != nil {
		return fmt.Errorf(lockClientErrMessage, err)
	}

	token, err := client.Lock(ctx, key)
	if err != nil {
		return fmt.Errorf("could not acquire lock: %w", err)
	}

	_, err = fmt.Fprintln(c.Writer, token)
	return err
}
