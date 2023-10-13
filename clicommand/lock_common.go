package clicommand

import "github.com/urfave/cli"

const lockClientErrMessage = `Could not connect to Agent API: %v
This command can only be used when at least one agent is running with the
"agent-api" experiment enabled.
`

// Flags used by all lock subcommands.
var lockCommonFlags = []cli.Flag{
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
