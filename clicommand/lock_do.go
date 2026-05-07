package clicommand

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/buildkite/agent/v3/lock"
	"github.com/urfave/cli/v3"
)

const lockDoHelpDescription = `Usage:

    buildkite-agent lock do [key]

Description:

Begins a do-once lock. Do-once can be used by multiple processes to
wait for completion of some shared work, where only one process should do
the work.

Note that this subcommand is only available when an agent has been started
with the ′agent-api′ experiment enabled.

′lock do′ will do one of two things:

- Print 'do'. The calling process should proceed to do the work and then
  call ′lock done′.
- Wait until the work is marked as done (with ′lock done′) and print 'done'.

If ′lock do′ prints 'done' immediately, the work was already done.

Examples:

    #!/usr/bin/env bash
    if [[ $(buildkite-agent lock do llama) == 'do' ]]; then
      # your critical section here...
      buildkite-agent lock done llama
    fi`

type LockDoConfig struct {
	GlobalConfig
	LockCommonConfig

	LockWaitTimeout time.Duration `cli:"lock-wait-timeout"`
}

var LockDoCommand = &cli.Command{
	Name:        "do",
	Usage:       "Begins a do-once lock",
	Description: lockDoHelpDescription,
	Flags: append(lockCommonFlags(),
		&cli.DurationFlag{
			Name:    "lock-wait-timeout",
			Usage:   "Sets a maximum duration to wait for a lock before giving up",
			Sources: cli.EnvVars("BUILDKITE_LOCK_WAIT_TIMEOUT"),
		},
	),
	Action: lockDoAction,
}

func lockDoAction(ctx context.Context, c *cli.Command) error {
	if c.NArg() != 1 {
		_, _ = fmt.Fprint(c.ErrWriter, lockDoHelpDescription)
		return &SilentExitError{code: 1}
	}
	key := c.Args().Get(0)

	ctx, cfg, _, _, done := setupLoggerAndConfig[LockDoConfig](ctx, c)
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

	do, err := client.DoOnceStart(ctx, key)
	if err != nil {
		return fmt.Errorf("couldn't start do-once lock: %w", err)
	}

	if do {
		_, err = fmt.Fprintln(c.Writer, "do")
	} else {
		_, err = fmt.Fprintln(c.Writer, "done")
	}
	return err
}
