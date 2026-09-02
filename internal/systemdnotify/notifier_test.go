package systemdnotify

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

type fakeBackend struct {
	enabled   bool
	interval  time.Duration
	watchErr  error
	notifyOK  bool
	notifyErr error
	states    []string
}

func (b *fakeBackend) notifyEnabled() bool {
	return b.enabled
}

func (b *fakeBackend) notify(state string) (bool, error) {
	b.states = append(b.states, state)
	return b.notifyOK, b.notifyErr
}

func (b *fakeBackend) watchdogInterval() (time.Duration, error) {
	return b.interval, b.watchErr
}

func TestNotifierDisabled(t *testing.T) {
	t.Parallel()

	b := &fakeBackend{}
	n, err := newNotifier(b)
	if err != nil {
		t.Fatalf("newNotifier() error = %v, want nil", err)
	}

	if err := n.Watchdog(); err != nil {
		t.Errorf("disabled notifier returned error: %v", err)
	}
	if len(b.states) != 0 {
		t.Errorf("disabled notifier sent states %q, want none", b.states)
	}
}

func TestNotifierWatchdog(t *testing.T) {
	t.Parallel()

	b := &fakeBackend{
		enabled:  true,
		interval: 3 * time.Minute,
		notifyOK: true,
	}
	n, err := newNotifier(b)
	if err != nil {
		t.Fatalf("newNotifier() error = %v, want nil", err)
	}

	if got, want := n.WatchdogInterval(), 3*time.Minute; got != want {
		t.Errorf("WatchdogInterval() = %v, want %v", got, want)
	}
	if err := n.Watchdog(); err != nil {
		t.Fatalf("Watchdog() error = %v, want nil", err)
	}
	if got, want := b.states, []string{watchdogState}; !slices.Equal(got, want) {
		t.Errorf("notification states = %q, want %q", got, want)
	}
}

func TestNotifierRejectsWatchdogWithoutSocket(t *testing.T) {
	t.Parallel()

	_, err := newNotifier(&fakeBackend{interval: time.Minute})
	if err == nil || !strings.Contains(err.Error(), "NOTIFY_SOCKET") {
		t.Fatalf("newNotifier() error = %v, want error mentioning NOTIFY_SOCKET", err)
	}
}

func TestNotifierReportsConfigurationError(t *testing.T) {
	t.Parallel()

	want := errors.New("invalid watchdog interval")
	_, err := newNotifier(&fakeBackend{watchErr: want})
	if !errors.Is(err, want) {
		t.Fatalf("newNotifier() error = %v, want error wrapping %v", err, want)
	}
}

func TestNotifierReportsSendFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("socket unavailable")
	n, err := newNotifier(&fakeBackend{
		enabled:   true,
		interval:  time.Minute,
		notifyErr: want,
	})
	if err != nil {
		t.Fatalf("newNotifier() error = %v, want nil", err)
	}

	if err := n.Watchdog(); !errors.Is(err, want) {
		t.Errorf("Watchdog() error = %v, want error wrapping %v", err, want)
	}
}
