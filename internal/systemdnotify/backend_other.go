//go:build !linux

package systemdnotify

import "time"

type noopBackend struct{}

func newBackend() backend {
	return noopBackend{}
}

func (noopBackend) notifyEnabled() bool {
	return false
}

func (noopBackend) notify(string) (bool, error) {
	return false, nil
}

func (noopBackend) watchdogInterval() (time.Duration, error) {
	return 0, nil
}
