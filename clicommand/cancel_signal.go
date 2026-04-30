package clicommand

import (
	"time"

	"github.com/urfave/cli"
)

const (
	defaultCancelSignalTimeout  = 10 * time.Second
	defaultCancelCleanupTimeout = 5 * time.Second
)

var (
	cancelSignalTimeoutFlag = cli.DurationFlag{
		Name:   "cancel-signal-timeout",
		Value:  defaultCancelSignalTimeout,
		Usage:  "The amount of time given to a subprocess to handle the cancel signal before SIGKILL is sent",
		EnvVar: "BUILDKITE_CANCEL_SIGNAL_TIMEOUT",
	}
	cancelSignalFlag = cli.StringFlag{
		Name:   "cancel-signal",
		Value:  "SIGTERM",
		Usage:  "The signal to use for cancellation",
		EnvVar: "BUILDKITE_CANCEL_SIGNAL",
	}
	cancelCleanupTimeoutFlag = cli.DurationFlag{
		Name:   "cancel-cleanup-timeout",
		Value:  defaultCancelCleanupTimeout,
		Usage:  "The amount of time given to the agent after the process exits or is killed to upload logs and artifacts",
		EnvVar: "BUILDKITE_CANCEL_CLEANUP_TIMEOUT",
	}
)
