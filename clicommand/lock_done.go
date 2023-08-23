package clicommand

import (
	"context"
	"fmt"
	"os"

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

    #!/bin/bash
    if [[ $(buildkite-agent lock do llama) == 'do' ]]; then
      # your critical section here...
      buildkite-agent lock done llama
    fi`

type LockDoneConfig struct {
	// Common config options
	LockScope   string `cli:"lock-scope"`
	SocketsPath string `cli:"sockets-path" normalize:"filepath"`

	// Global flags
	Debug       bool     `cli:"debug"`
	LogLevel    string   `cli:"log-level"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`
}

var LockDoneCommand = cli.Command{
	Name:        "done",
	Usage:       "Completes a do-once lock",
	Description: lockDoneHelpDescription,
	Flags:       append(globalFlags(), lockCommonFlags...),
	Action:      lockDoneAction,
}

func lockDoneAction(c *cli.Context) error {
	if c.NArg() != 1 {
		fmt.Fprint(c.App.ErrWriter, lockDoneHelpDescription)
		os.Exit(1)
	}
	key := c.Args()[0]

	ctx := context.Background()
	cfg, l, _, done := setupLoggerAndConfig[LockDoneConfig](c)
	defer done()

	if cfg.LockScope != "machine" {
		l.Fatal("Only 'machine' scope for locks is supported in this version.")
	}

	client, err := lock.NewClient(ctx, cfg.SocketsPath)
	if err != nil {
		l.Fatal(lockClientErrMessage, err)
	}

	if err := client.DoOnceEnd(ctx, key); err != nil {
		l.Fatal("Couldn't complete do-once lock: %v", err)
	}

	return nil
}
