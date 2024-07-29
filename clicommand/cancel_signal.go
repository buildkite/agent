package clicommand

import (
	"fmt"
	"time"

	"github.com/urfave/cli"
)

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

// signalGracePeriod computes the signal grace period based on the various
// possible flag configurations:
//   - If signalGracePeriodSecs is negative, it is relative to
//     cancelGracePeriodSecs.
//   - If cancelGracePeriodSecs is less than signalGracePeriodSecs that is an
//     error.
func signalGracePeriod(cancelGracePeriodSecs, signalGracePeriodSecs int) (time.Duration, error) {
	// Treat a negative signal grace period as relative to the cancel grace period
	if signalGracePeriodSecs < 0 {
		if cancelGracePeriodSecs < -signalGracePeriodSecs {
			return 0, fmt.Errorf(
				"cancel-grace-period (%d) must be at least as big as signal-grace-period-seconds (%d)",
				cancelGracePeriodSecs,
				signalGracePeriodSecs,
			)
		}
		signalGracePeriodSecs = cancelGracePeriodSecs + signalGracePeriodSecs
	}

	if cancelGracePeriodSecs <= signalGracePeriodSecs {
		return 0, fmt.Errorf(
			"cancel-grace-period (%d) must be greater than signal-grace-period-seconds (%d)",
			cancelGracePeriodSecs,
			signalGracePeriodSecs,
		)
	}

	return time.Duration(signalGracePeriodSecs) * time.Second, nil
}
