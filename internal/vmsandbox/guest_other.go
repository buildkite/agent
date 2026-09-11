//go:build !linux

package vmsandbox

import (
	"context"
	"errors"
	"time"

	"github.com/buildkite/agent/v4/logger"
)

// GuestConfig configures RunGuest. See guest_linux.go.
type GuestConfig struct {
	ConnectTimeout time.Duration
	PowerOff       func()
}

// RunGuest only works inside a Linux guest; this stub keeps the CLI
// building everywhere else.
func RunGuest(context.Context, logger.Logger, GuestConfig) error {
	return errors.New("vm-guest-bootstrap only runs inside a Linux sandbox guest")
}

// PowerOffGuest is a no-op off Linux.
func PowerOffGuest() {}
