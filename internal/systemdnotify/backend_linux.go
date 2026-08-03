//go:build linux

package systemdnotify

import (
	"os"
	"time"

	"github.com/coreos/go-systemd/v22/daemon"
)

type systemdBackend struct{}

func newBackend() backend {
	return systemdBackend{}
}

func (systemdBackend) notifyEnabled() bool {
	return os.Getenv("NOTIFY_SOCKET") != ""
}

func (systemdBackend) notify(state string) (bool, error) {
	return daemon.SdNotify(false, state)
}

func (systemdBackend) watchdogInterval() (time.Duration, error) {
	return daemon.SdWatchdogEnabled(false)
}
