package clicommand

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/buildkite/agent/v4/internal/vmsandbox"
	"github.com/urfave/cli/v3"
)

const vmGuestBootstrapHelpDescription = `Usage:

     buildkite-agent vm-guest-bootstrap [options...]

Description:

This command is used internally by the agent's VM sandbox mode. It runs as
the guest's init-launched helper: it connects back to the host agent over
vsock, receives the job's bootstrap environment, runs "buildkite-agent
bootstrap", streams its output to the host, reports the exit status, and
powers the guest off. It is not intended to be used directly.`

type VMGuestBootstrapConfig struct {
	ConnectTimeout time.Duration `cli:"vm-guest-connect-timeout"`
	NoPowerOff     bool          `cli:"vm-guest-no-power-off"`

	// Global flags for debugging, etc
	LogLevel    string   `cli:"log-level"`
	Debug       bool     `cli:"debug"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`
}

var VMGuestBootstrapCommand = &cli.Command{
	Name:        "vm-guest-bootstrap",
	Usage:       "Harness used internally by the agent to run jobs inside a sandbox VM",
	Category:    categoryInternal,
	Description: vmGuestBootstrapHelpDescription,
	Flags: []cli.Flag{
		&cli.DurationFlag{
			Name:    "vm-guest-connect-timeout",
			Usage:   "How long to keep trying to connect to the host agent over vsock",
			Value:   30 * time.Second,
			Sources: cli.EnvVars("BUILDKITE_VM_GUEST_CONNECT_TIMEOUT"),
		},
		&cli.BoolFlag{
			Name:    "vm-guest-no-power-off",
			Usage:   "Exit instead of powering off the machine when done (for testing the helper outside a guest)",
			Sources: cli.EnvVars("BUILDKITE_VM_GUEST_NO_POWER_OFF"),
		},

		// Global flags for debugging, etc
		DebugFlag,
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		ctx, cfg, l, _, done := setupLoggerAndConfig[VMGuestBootstrapConfig](ctx, c)
		defer done()

		// Cancellation comes from the host over vsock, not from signals.
		// Swallow the usual ones so a stray signal inside the guest can't
		// bypass the host's view of the job.
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
		go func() {
			for sig := range signals {
				l.Infof("vm-guest-bootstrap: Received %v; awaiting interrupt from host", sig)
			}
		}()

		powerOff := vmsandbox.PowerOffGuest
		if cfg.NoPowerOff {
			powerOff = func() {}
		}
		if err := vmsandbox.RunGuest(ctx, l, vmsandbox.GuestConfig{
			ConnectTimeout: cfg.ConnectTimeout,
			PowerOff:       powerOff,
		}); err != nil {
			return &ExitError{1, err}
		}
		return NewSilentExitError(0)
	},
}
