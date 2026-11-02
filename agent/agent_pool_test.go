package agent

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
)

func TestAgentPoolHTTPHealth(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tests := []struct {
		name                 string
		watchdogRunningSince time.Time
		lastHeartbeat        time.Time
		lastHeartbeatError   error
		watchdogErr          error
		wantStatus           int
		wantBody             string
	}{
		{
			name:       "connecting worker",
			wantStatus: http.StatusOK,
			wantBody:   "OK:",
		},
		{
			name:                 "awaiting first heartbeat within grace",
			watchdogRunningSince: now.Add(-time.Minute),
			wantStatus:           http.StatusOK,
			wantBody:             "OK:",
		},
		{
			name:                 "first heartbeat never arrived",
			watchdogRunningSince: now.Add(-3 * time.Minute),
			wantStatus:           http.StatusServiceUnavailable,
			wantBody:             "worker 2 has not successfully contacted Buildkite",
		},
		{
			name:                 "recent successful heartbeat despite transient error",
			watchdogRunningSince: now.Add(-3 * time.Minute),
			lastHeartbeat:        now.Add(-time.Minute),
			lastHeartbeatError:   errors.New("temporary network failure"),
			wantStatus:           http.StatusOK,
			wantBody:             "OK:",
		},
		{
			name:                 "stale heartbeat without recorded error",
			watchdogRunningSince: now.Add(-4 * time.Minute),
			lastHeartbeat:        now.Add(-3 * time.Minute),
			wantStatus:           http.StatusServiceUnavailable,
			wantBody:             "worker 2 has not successfully contacted Buildkite",
		},
		{
			name:          "failed worker despite recent heartbeat",
			lastHeartbeat: now,
			watchdogErr:   errors.New("worker stopped unexpectedly"),
			wantStatus:    http.StatusServiceUnavailable,
			wantBody:      "worker 2 failed: worker stopped unexpectedly",
		},
		{
			name:          "intentionally stopped worker with old heartbeat",
			lastHeartbeat: now.Add(-time.Hour),
			wantStatus:    http.StatusOK,
			wantBody:      "OK:",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// A healthy sibling must not mask a failed or stale worker.
			pool := &AgentPool{workers: []*AgentWorker{
				{
					spawnIndex: 1,
					agent:      &api.AgentRegisterResponse{HeartbeatInterval: 60},
					stats: agentStats{
						watchdogRunningSince: now.Add(-time.Minute),
						lastHeartbeat:        now,
					},
				},
				{
					spawnIndex: 2,
					agent:      &api.AgentRegisterResponse{HeartbeatInterval: 60},
					stats: agentStats{
						watchdogRunningSince: test.watchdogRunningSince,
						lastHeartbeat:        test.lastHeartbeat,
						lastHeartbeatError:   test.lastHeartbeatError,
						watchdogErr:          test.watchdogErr,
					},
				},
			}}
			response := httptest.NewRecorder()
			pool.healthHandler(logger.Discard).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
			if got := response.Code; got != test.wantStatus {
				t.Errorf("HTTP status = %d, want %d; body: %s", got, test.wantStatus, response.Body.String())
			}
			if got := response.Body.String(); !strings.Contains(got, test.wantBody) {
				t.Errorf("HTTP body = %q, want containing %q", got, test.wantBody)
			}
		})
	}
}

func TestAgentPoolHTTPHealthRecovery(t *testing.T) {
	t.Parallel()

	for _, protocol := range []string{"HTTP/1.1", "HTTP/2.0"} {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()

			worker := &AgentWorker{
				spawnIndex: 1,
				agent:      &api.AgentRegisterResponse{HeartbeatInterval: 60},
			}
			worker.watchdogMarkRunning()
			pool := &AgentPool{workers: []*AgentWorker{worker}}
			server := httptest.NewUnstartedServer(pool.healthHandler(logger.Discard))
			server.Config.Protocols = new(http.Protocols)
			server.Config.Protocols.SetHTTP1(true)
			server.Config.Protocols.SetUnencryptedHTTP2(true)
			server.Start()
			t.Cleanup(server.Close)

			transport := &http.Transport{Protocols: new(http.Protocols)}
			transport.Protocols.SetHTTP1(protocol == "HTTP/1.1")
			transport.Protocols.SetUnencryptedHTTP2(protocol == "HTTP/2.0")
			t.Cleanup(transport.CloseIdleConnections)
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			check := func(path string, wantStatus int) {
				t.Helper()
				response, err := client.Get(server.URL + path)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := response.Body.Close(); err != nil {
						t.Errorf("close response body: %v", err)
					}
				}()
				if _, err := io.Copy(io.Discard, response.Body); err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != wantStatus || response.Proto != protocol {
					t.Errorf("GET %s = %s %d, want %s %d", path, response.Proto, response.StatusCode, protocol, wantStatus)
				}
			}

			check("/", http.StatusOK)
			worker.stats.Lock()
			worker.stats.watchdogRunningSince = time.Now().Add(-4 * time.Minute)
			worker.stats.lastHeartbeat = time.Now().Add(-3 * time.Minute)
			worker.stats.Unlock()
			check("/", http.StatusServiceUnavailable)
			worker.stats.Lock()
			worker.stats.lastHeartbeat = time.Now()
			worker.stats.Unlock()
			check("/", http.StatusOK)
			check("/missing", http.StatusNotFound)
		})
	}
}
