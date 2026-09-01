package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/api"
)

type fakeWatchdogNotifier struct {
	interval time.Duration

	mu       sync.Mutex
	watchdog int
	watchErr error
}

func (n *fakeWatchdogNotifier) Watchdog() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.watchdog++
	return n.watchErr
}

func (n *fakeWatchdogNotifier) WatchdogInterval() time.Duration {
	return n.interval
}

func (n *fakeWatchdogNotifier) watchdogCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.watchdog
}

func TestAgentWorkerWatchdogHealth(t *testing.T) {
	t.Parallel()

	now := time.Now()
	failure := errors.New("heartbeat rejected")
	tests := []struct {
		name                 string
		heartbeatInterval    int
		lastHeartbeat        time.Time
		watchdogRunningSince time.Time
		watchdogErr          error
		wantError            string
	}{
		{
			name:              "connecting or stopped worker",
			heartbeatInterval: 60,
		},
		{
			name:                 "running worker within two heartbeat intervals",
			heartbeatInterval:    60,
			lastHeartbeat:        now.Add(-119 * time.Second),
			watchdogRunningSince: now.Add(-3 * time.Minute),
		},
		{
			name:                 "running worker beyond two heartbeat intervals",
			heartbeatInterval:    60,
			lastHeartbeat:        now.Add(-121 * time.Second),
			watchdogRunningSince: now.Add(-3 * time.Minute),
			wantError:            "has not successfully contacted Buildkite",
		},
		{
			name:              "failed worker",
			heartbeatInterval: 60,
			watchdogErr:       failure,
			wantError:         failure.Error(),
		},
		{
			name:                 "running worker awaiting its first heartbeat",
			heartbeatInterval:    60,
			watchdogRunningSince: now.Add(-time.Minute),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			worker := &AgentWorker{
				spawnIndex: 3,
				agent: &api.AgentRegisterResponse{
					HeartbeatInterval: test.heartbeatInterval,
				},
				stats: agentStats{
					lastHeartbeat:        test.lastHeartbeat,
					watchdogRunningSince: test.watchdogRunningSince,
					watchdogErr:          test.watchdogErr,
				},
			}
			err := worker.watchdogHealth(now)
			if test.wantError == "" {
				if err != nil {
					t.Errorf("watchdogHealth() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Errorf("watchdogHealth() error = %v, want error containing %q", err, test.wantError)
			}
		})
	}
}

func TestAgentWorkerSuccessfulHeartbeatDoesNotReviveFailedWorker(t *testing.T) {
	t.Parallel()

	worker := &AgentWorker{
		agent: &api.AgentRegisterResponse{HeartbeatInterval: 60},
	}
	worker.watchdogMarkFailed(errors.New("failed"))
	worker.stats.Lock()
	worker.stats.lastHeartbeat = time.Now()
	worker.stats.Unlock()

	if err := worker.watchdogHealth(time.Now()); err == nil {
		t.Fatal("watchdogHealth() error = nil, want failed worker error")
	}
}

func TestAgentWorkerStoppedPreservesWatchdogFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("failed")
	worker := &AgentWorker{
		agent: &api.AgentRegisterResponse{HeartbeatInterval: 60},
	}
	worker.watchdogMarkRunning()
	worker.watchdogMarkFailed(want)
	worker.watchdogMarkStopped()

	if err := worker.watchdogHealth(time.Now()); !errors.Is(err, want) {
		t.Errorf("watchdogHealth() error = %v, want error wrapping %v", err, want)
	}

	worker.watchdogMarkRunning()
	if err := worker.watchdogHealth(time.Now()); err != nil {
		t.Errorf("watchdogHealth() after restart error = %v, want nil", err)
	}
}

func TestAgentWorkerHeartbeatRecordsOnlySuccessfulBuildkiteContact(t *testing.T) {
	t.Parallel()

	var rejectHeartbeat atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rejectHeartbeat.Load() {
			http.Error(w, "heartbeat rejected", http.StatusUnauthorized)
			return
		}

		var heartbeat api.Heartbeat
		if err := json.NewDecoder(r.Body).Decode(&heartbeat); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		heartbeat.ReceivedAt = time.Now().Format(time.RFC3339Nano)
		if err := json.NewEncoder(w).Encode(heartbeat); err != nil {
			t.Errorf("Encode(heartbeat) error = %v", err)
		}
	}))
	t.Cleanup(server.Close)

	worker := &AgentWorker{
		agent: &api.AgentRegisterResponse{HeartbeatInterval: 60},
		apiClient: api.NewClient(slog.New(slog.DiscardHandler), api.Config{
			Endpoint: server.URL,
			Token:    "test-token",
		}),
		logger: slog.New(slog.DiscardHandler),
		stats: agentStats{
			lastHeartbeat:        time.Now().Add(-time.Minute),
			watchdogRunningSince: time.Now().Add(-2 * time.Minute),
		},
	}
	lastSuccessfulContact := func() time.Time {
		worker.stats.Lock()
		defer worker.stats.Unlock()
		return worker.stats.lastHeartbeat
	}

	beforeSuccess := lastSuccessfulContact()
	if err := worker.Heartbeat(t.Context()); err != nil {
		t.Fatalf("Heartbeat() error = %v, want nil", err)
	}
	afterSuccess := lastSuccessfulContact()
	if !afterSuccess.After(beforeSuccess) {
		t.Errorf("lastSuccessfulContact after successful heartbeat = %s, want after %s", afterSuccess, beforeSuccess)
	}

	rejectHeartbeat.Store(true)
	if err := worker.Heartbeat(t.Context()); err == nil {
		t.Fatal("Heartbeat() error = nil, want heartbeat rejection")
	}
	if got := lastSuccessfulContact(); got != afterSuccess {
		t.Errorf("lastSuccessfulContact after rejected heartbeat = %s, want unchanged %s", got, afterSuccess)
	}
}

func TestAgentPoolWatchdogNotifiesForHealthyWorkers(t *testing.T) {
	t.Parallel()

	now := time.Now()
	worker := &AgentWorker{
		agent: &api.AgentRegisterResponse{HeartbeatInterval: 60},
		stats: agentStats{
			lastHeartbeat:        now,
			watchdogRunningSince: now.Add(-time.Minute),
		},
	}
	notifier := &fakeWatchdogNotifier{}
	pool := &AgentPool{
		workers:  []*AgentWorker{worker},
		watchdog: notifier,
	}

	pool.notifyWatchdog(slog.New(slog.DiscardHandler), now.Add(time.Minute))
	if got := notifier.watchdogCount(); got != 1 {
		t.Errorf("Watchdog() calls = %d, want 1", got)
	}
}

func TestAgentPoolWatchdogSkipsUnhealthyWorkers(t *testing.T) {
	t.Parallel()

	now := time.Now()
	notifier := &fakeWatchdogNotifier{}
	pool := &AgentPool{
		workers: []*AgentWorker{{
			agent: &api.AgentRegisterResponse{HeartbeatInterval: 60},
			stats: agentStats{
				lastHeartbeat:        now.Add(-121 * time.Second),
				watchdogRunningSince: now.Add(-3 * time.Minute),
			},
		}},
		watchdog: notifier,
	}
	pool.notifyWatchdog(slog.New(slog.DiscardHandler), now)
	if got := notifier.watchdogCount(); got != 0 {
		t.Errorf("Watchdog() calls = %d, want 0", got)
	}
}

func TestAgentPoolWatchdogContinuesAfterNotificationError(t *testing.T) {
	t.Parallel()

	now := time.Now()
	notifier := &fakeWatchdogNotifier{
		watchErr: errors.New("send failed"),
	}
	pool := &AgentPool{
		workers: []*AgentWorker{{
			agent: &api.AgentRegisterResponse{HeartbeatInterval: 60},
			stats: agentStats{
				lastHeartbeat:        now,
				watchdogRunningSince: now.Add(-time.Minute),
			},
		}},
		watchdog: notifier,
	}

	pool.notifyWatchdog(slog.New(slog.DiscardHandler), now)
	pool.notifyWatchdog(slog.New(slog.DiscardHandler), now)
	if got := notifier.watchdogCount(); got != 2 {
		t.Errorf("Watchdog() calls = %d, want 2", got)
	}
}

func TestAgentPoolWatchdogNotifiesImmediately(t *testing.T) {
	t.Parallel()

	notifier := &fakeWatchdogNotifier{}
	pool := &AgentPool{watchdog: notifier}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	pool.runWatchdog(ctx, slog.New(slog.DiscardHandler), time.Hour)
	if got := notifier.watchdogCount(); got != 1 {
		t.Errorf("Watchdog() calls = %d, want 1", got)
	}
}

func TestAgentPoolWatchdogLoopStops(t *testing.T) {
	t.Parallel()

	pool := &AgentPool{}
	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		pool.runWatchdogLoop(ctx, slog.New(slog.DiscardHandler), ticks)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchdog loop did not stop after context cancellation")
	}
}
