// Package systemdnotify reports service liveness to systemd.
package systemdnotify

import (
	"errors"
	"fmt"
	"time"
)

const watchdogState = "WATCHDOG=1"

type backend interface {
	notifyEnabled() bool
	notify(state string) (bool, error)
	watchdogInterval() (time.Duration, error)
}

// Notifier reports agent liveness to its systemd service manager. It is inert
// when the systemd watchdog is disabled.
type Notifier struct {
	backend          backend
	watchdogDuration time.Duration
}

// New returns a notifier configured from the current systemd environment.
func New() (*Notifier, error) {
	return newNotifier(newBackend())
}

func newNotifier(b backend) (*Notifier, error) {
	watchdogDuration, err := b.watchdogInterval()
	if err != nil {
		return nil, fmt.Errorf("read systemd watchdog configuration: %w", err)
	}

	notifyEnabled := b.notifyEnabled()
	if watchdogDuration > 0 && !notifyEnabled {
		return nil, errors.New("systemd watchdog is enabled but NOTIFY_SOCKET is not set")
	}

	return &Notifier{
		backend:          b,
		watchdogDuration: watchdogDuration,
	}, nil
}

// WatchdogInterval returns the maximum time systemd permits between watchdog
// notifications. A zero duration means the watchdog is disabled.
func (n *Notifier) WatchdogInterval() time.Duration {
	return n.watchdogDuration
}

// Watchdog tells systemd that the agent remains healthy.
func (n *Notifier) Watchdog() error {
	if n.watchdogDuration <= 0 {
		return nil
	}
	return n.notify(watchdogState)
}

func (n *Notifier) notify(state string) error {
	sent, err := n.backend.notify(state)
	if err != nil {
		return fmt.Errorf("send systemd notification %q: %w", state, err)
	}
	if !sent {
		return fmt.Errorf("send systemd notification %q: notification socket is unavailable", state)
	}
	return nil
}
