package agent

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/logtest"
	"github.com/buildkite/agent/v4/metrics"
)

const gracefulStopTestSessionToken = "agent-session-token"

type gracefulStopTestServer struct {
	*httptest.Server

	t            *testing.T
	stopStatus   int
	releaseStop  <-chan struct{}
	stopStarted  chan struct{}
	disconnected chan struct{}

	stopStartedOnce  sync.Once
	disconnectedOnce sync.Once

	mu           sync.Mutex
	events       []string
	stopRequests []api.AgentStopRequest
}

func newGracefulStopTestServer(t *testing.T, stopStatus int, releaseStop <-chan struct{}) *gracefulStopTestServer {
	t.Helper()

	s := &gracefulStopTestServer{
		t:            t,
		stopStatus:   stopStatus,
		releaseStop:  releaseStop,
		stopStarted:  make(chan struct{}),
		disconnected: make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /stop", s.handleStop)
	mux.HandleFunc("POST /disconnect", s.handleDisconnect)
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func (s *gracefulStopTestServer) checkSessionToken(req *http.Request) {
	s.t.Helper()
	if got, want := req.Header.Get("Authorization"), "Token "+gracefulStopTestSessionToken; got != want {
		s.t.Errorf("Authorization header = %q, want %q", got, want)
	}
}

func (s *gracefulStopTestServer) handleStop(rw http.ResponseWriter, req *http.Request) {
	s.checkSessionToken(req)

	var stopReq api.AgentStopRequest
	if err := json.NewDecoder(req.Body).Decode(&stopReq); err != nil {
		s.t.Errorf("decoding graceful stop request: %v", err)
		http.Error(rw, "invalid request", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.stopRequests = append(s.stopRequests, stopReq)
	s.mu.Unlock()
	s.stopStartedOnce.Do(func() { close(s.stopStarted) })

	if s.releaseStop != nil {
		select {
		case <-s.releaseStop:
		case <-req.Context().Done():
			return
		}
	}

	s.mu.Lock()
	s.events = append(s.events, "stop")
	s.mu.Unlock()
	rw.WriteHeader(s.stopStatus)
}

func (s *gracefulStopTestServer) handleDisconnect(rw http.ResponseWriter, req *http.Request) {
	s.checkSessionToken(req)
	s.mu.Lock()
	s.events = append(s.events, "disconnect")
	s.mu.Unlock()
	s.disconnectedOnce.Do(func() { close(s.disconnected) })
	rw.WriteHeader(http.StatusOK)
}

func (s *gracefulStopTestServer) snapshot() (events []string, stopRequests []api.AgentStopRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.events), slices.Clone(s.stopRequests)
}

func newGracefulStopTestWorker(t *testing.T, server *gracefulStopTestServer) (*AgentWorker, *logtest.Handler) {
	t.Helper()

	l, logHandler := logtest.NewLogger()
	return NewAgentWorker(
		l,
		&api.AgentRegisterResponse{
			UUID:        "agent-id",
			Name:        "agent-name",
			AccessToken: gracefulStopTestSessionToken,
			Endpoint:    server.URL,
		},
		metrics.NewCollector(slog.New(slog.DiscardHandler), metrics.CollectorConfig{}),
		api.NewClient(slog.New(slog.DiscardHandler), api.Config{
			Endpoint: server.URL,
			Token:    "registration-token",
		}),
		AgentWorkerConfig{},
	), logHandler
}

func TestAgentWorker_ReportsGracefulStopBeforeDisconnect(t *testing.T) {
	t.Parallel()

	releaseStop := make(chan struct{})
	server := newGracefulStopTestServer(t, http.StatusOK, releaseStop)
	worker, _ := newGracefulStopTestWorker(t, server)

	worker.StopGracefully()
	worker.StopGracefully()
	select {
	case <-server.stopStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for graceful stop request")
	}

	done := make(chan error, 1)
	go func() {
		done <- worker.Disconnect(t.Context())
	}()
	select {
	case <-server.disconnected:
		t.Fatal("worker disconnected before graceful stop request completed")
	case err := <-done:
		t.Fatalf("Disconnect() returned before graceful stop request completed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseStop)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Disconnect() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for agent worker to disconnect")
	}

	events, stopRequests := server.snapshot()
	if got, want := events, []string{"stop", "disconnect"}; !slices.Equal(got, want) {
		t.Errorf("request events = %v, want %v", got, want)
	}
	if got, want := len(stopRequests), 1; got != want {
		t.Fatalf("graceful stop request count = %d, want %d", got, want)
	}
	if stopRequests[0].Force {
		t.Error("graceful stop request Force = true, want false")
	}
}

func TestAgentWorker_GracefulStopReportFailureDoesNotPreventDisconnect(t *testing.T) {
	t.Parallel()

	server := newGracefulStopTestServer(t, http.StatusInternalServerError, nil)
	worker, logHandler := newGracefulStopTestWorker(t, server)

	worker.StopGracefully()
	if err := worker.Disconnect(t.Context()); err != nil {
		t.Fatalf("Disconnect() error = %v, want nil", err)
	}

	events, _ := server.snapshot()
	if got, want := events, []string{"stop", "disconnect"}; !slices.Equal(got, want) {
		t.Errorf("request events = %v, want %v", got, want)
	}
	if !slices.ContainsFunc(logHandler.Messages(), func(message string) bool {
		return strings.Contains(message, "Failed to report graceful stop to Buildkite")
	}) {
		t.Errorf("log messages = %v, want graceful stop reporting warning", logHandler.Messages())
	}
}

func TestAgentWorker_DoesNotReportOtherStops(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		stop func(*AgentWorker)
	}{
		{
			name: "ungraceful stop",
			stop: (*AgentWorker).StopUngracefully,
		},
		{
			name: "no stop requested",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server := newGracefulStopTestServer(t, http.StatusOK, nil)
			worker, _ := newGracefulStopTestWorker(t, server)
			if test.stop != nil {
				test.stop(worker)
			}

			if err := worker.Disconnect(t.Context()); err != nil {
				t.Fatalf("Disconnect() error = %v, want nil", err)
			}

			events, _ := server.snapshot()
			if got, want := events, []string{"disconnect"}; !slices.Equal(got, want) {
				t.Errorf("request events = %v, want %v", got, want)
			}
		})
	}
}
