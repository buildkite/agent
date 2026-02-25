package clicommand

import "github.com/urfave/cli"

const lockClientErrMessage = `could not connect to Agent API: %v
this command can only be used when at least one agent is running with the
"agent-api" experiment enabled`

// Flags used by all lock subcommands.
func lockCommonFlags() []cli.Flag {
	return append(globalFlags(),
		cli.StringFlag{
			Name:   "lock-scope",
			Value:  "machine",
			Usage:  "The scope for locks used in this operation. Currently only 'machine' scope is supported",
			EnvVar: "BUILDKITE_LOCK_SCOPE",
		},
		SocketsPathFlag,
	)
}

type LockCommonConfig struct {
	LockScope   string `cli:"lock-scope"`
	SocketsPath string `cli:"sockets-path" normalize:"filepath"`
}
