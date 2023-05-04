package clicommand

import (
	"context"
	"time"

	"github.com/urfave/cli"
)

const lockClientErrMessage = `Could not connect to Agent API: %v
This command can only be used when at least one agent is running with the
"agent-api" experiment enabled.
`

// Flags used by all lock subcommands.
var lockCommonFlags = []cli.Flag{
	cli.StringFlag{
		Name:   "config",
		Value:  "",
		Usage:  "Path to a configuration file",
		EnvVar: "BUILDKITE_AGENT_CONFIG",
	},

	cli.StringFlag{
		Name:   "lock-scope",
		Value:  "machine",
		Usage:  "The scope for locks used in this operation. Currently only 'machine' scope is supported",
		EnvVar: "BUILDKITE_LOCK_SCOPE",
	},
	cli.StringFlag{
		Name:   "sockets-path",
		Value:  defaultSocketsPath(),
		Usage:  "Directory where the agent will place sockets",
		EnvVar: "BUILDKITE_SOCKETS_PATH",
	},
}

// sleep sleeps in a context-aware way. The only non-nil errors returned are
// from ctx.Err.
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
