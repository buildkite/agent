package clicommand

import "github.com/urfave/cli"

const (
	defaultCancelGracePeriod = 10
)

var (
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
			"After this period has elapsed, SIGKILL will be sent. " +
			"Negative values are taken relative to ′cancel-grace-period′. " +
			"The default is ′cancel-grace-period′ - 1.",
		EnvVar: "BUILDKITE_SIGNAL_GRACE_PERIOD_SECONDS",
		Value:  -1,
	}
)
