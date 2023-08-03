package clicommand

import "github.com/urfave/cli"

const (
	defaultCancelGracePeriod = 10

	// This will be increased to 9 in a future release of the agent.
	defaultSignalGracePeriod = 0
)

var (
	// cancel grace period must be strictly longer than signal grace period
	_ uint = defaultCancelGracePeriod - defaultSignalGracePeriod - 1

	cancelGracePeriodFlag = cli.IntFlag{
		Name:  "cancel-grace-period",
		Value: defaultCancelGracePeriod,
		Usage: "The number of seconds a canceled or timed out job is given " +
			"to gracefully terminate and upload its artifacts",
		EnvVar: "BUILDKITE_CANCEL_GRACE_PERIOD",
	}
	cancelSignalFlag = cli.StringFlag{
		Name:   "cancel-signal",
		Usage:  "The signal to use for cancellation",
		EnvVar: "BUILDKITE_CANCEL_SIGNAL",
		Value:  "SIGTERM",
	}
	signalGracePeriodSecondsFlag = cli.IntFlag{
		Name: "signal-grace-period-seconds",
		Usage: "The number of seconds given to a subprocess to handle being sent ′cancel-signal′. " +
			"After this period has elaspsed, SIGKILL will be sent.",
		EnvVar: "BUILDKITE_SIGNAL_GRACE_PERIOD_SECONDS",
		Value:  defaultSignalGracePeriod,
	}
)
