package clicommand

import (
	"time"

	"github.com/urfave/cli/v3"
)

const (
	defaultCancelSignalTimeout  = 10 * time.Second
	defaultCancelCleanupTimeout = 5 * time.Second
)

var (
	cancelSignalTimeoutFlag = &cli.DurationFlag{
		Name:    "cancel-signal-timeout",
		Value:   defaultCancelSignalTimeout,
		Usage:   "The amount of time given to a subprocess to handle the cancel signal before SIGKILL is sent",
		Sources: cli.EnvVars("BUILDKITE_CANCEL_SIGNAL_TIMEOUT"),
	}
	cancelSignalFlag = &cli.StringFlag{
		Name:    "cancel-signal",
		Value:   "SIGTERM",
		Usage:   "The signal to use for cancellation",
		Sources: cli.EnvVars("BUILDKITE_CANCEL_SIGNAL"),
	}
	cancelCleanupTimeoutFlag = &cli.DurationFlag{
		Name:    "cancel-cleanup-timeout",
		Value:   defaultCancelCleanupTimeout,
		Usage:   "Extra time for a stopping agent to upload logs and artifacts after a job process exits or is killed, before the agent forcefully exits",
		Sources: cli.EnvVars("BUILDKITE_CANCEL_CLEANUP_TIMEOUT"),
	}
)
