package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type watchdogNotifier interface {
	Watchdog() error
	WatchdogInterval() time.Duration
}

func (a *AgentWorker) watchdogMarkRunning() {
	a.stats.Lock()
	defer a.stats.Unlock()

	a.stats.watchdogRunningSince = time.Now()
	a.stats.watchdogErr = nil
}

func (a *AgentWorker) watchdogMarkStopped() {
	a.stats.Lock()
	defer a.stats.Unlock()

	a.stats.watchdogRunningSince = time.Time{}
}

func (a *AgentWorker) watchdogMarkFailed(err error) {
	a.stats.Lock()
	defer a.stats.Unlock()

	a.stats.watchdogRunningSince = time.Time{}
	a.stats.watchdogErr = err
}

func (a *AgentWorker) watchdogHealth(now time.Time) error {
	a.stats.Lock()
	defer a.stats.Unlock()

	if a.stats.watchdogErr != nil {
		return fmt.Errorf("worker %d failed: %w", a.spawnIndex, a.stats.watchdogErr)
	}
	if a.stats.watchdogRunningSince.IsZero() {
		// Workers that are still connecting or have intentionally stopped do
		// not contribute to pool health.
		return nil
	}

	lastSuccessfulContact := a.stats.lastHeartbeat
	if lastSuccessfulContact.Before(a.stats.watchdogRunningSince) {
		// Connect succeeded immediately before the worker started, so use it
		// as the heartbeat grace period until the first heartbeat succeeds.
		lastSuccessfulContact = a.stats.watchdogRunningSince
	}
	// A worker remains healthy through one delayed or in-flight heartbeat.
	maxContactAge := 2 * time.Duration(a.agent.HeartbeatInterval) * time.Second
	age := now.Sub(lastSuccessfulContact)
	if age > maxContactAge {
		return fmt.Errorf("worker %d has not successfully contacted Buildkite for %s", a.spawnIndex, age.Round(time.Second))
	}
	return nil
}

func (ap *AgentPool) runWatchdog(ctx context.Context, l *slog.Logger, watchdogInterval time.Duration) {
	ap.notifyWatchdog(l, time.Now())

	// systemd recommends notifying every half of the configured interval.
	ticker := time.NewTicker(watchdogInterval / 2)
	defer ticker.Stop()

	ap.runWatchdogLoop(ctx, l, ticker.C)
}

func (ap *AgentPool) runWatchdogLoop(ctx context.Context, l *slog.Logger, ticks <-chan time.Time) {
	for {
		select {
		case _, ok := <-ticks:
			if !ok {
				return
			}
			ap.notifyWatchdog(l, time.Now())
		case <-ctx.Done():
			return
		}
	}
}

func (ap *AgentPool) notifyWatchdog(l *slog.Logger, now time.Time) {
	if err := ap.watchdogHealth(now); err != nil {
		l.Warn("Buildkite heartbeat has not succeeded; not sending systemd watchdog notification", "error", err)
		return
	}
	if err := ap.watchdog.Watchdog(); err != nil {
		l.Warn("Failed to notify systemd watchdog", "error", err)
		return
	}
	l.Debug("Notified systemd watchdog")
}

func (ap *AgentPool) watchdogHealth(now time.Time) error {
	for _, worker := range ap.workers {
		if err := worker.watchdogHealth(now); err != nil {
			return err
		}
	}
	return nil
}
