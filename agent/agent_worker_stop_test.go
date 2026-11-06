package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/buildkite/agent/v4/api"
	agentedgev1 "github.com/buildkite/agent/v4/api/proto/gen"
	"github.com/buildkite/agent/v4/logger"
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

func newGracefulStopTestWorker(t *testing.T, endpoint string, l logger.Logger, cfg AgentWorkerConfig) *AgentWorker {
	t.Helper()

	worker := NewAgentWorker(
		l,
		&api.AgentRegisterResponse{
			UUID:              "agent-id",
			Name:              "agent-name",
			AccessToken:       gracefulStopTestSessionToken,
			Endpoint:          endpoint,
			PingInterval:      1,
			HeartbeatInterval: 60,
		},
		metrics.NewCollector(logger.Discard, metrics.CollectorConfig{}),
		api.NewClient(logger.Discard, api.Config{
			Endpoint: endpoint,
			Token:    "registration-token",
		}),
		cfg,
	)
	worker.noWaitBetweenPingsForTesting = true
	return worker
}

func TestAgentWorker_ReportsGracefulStopBeforeDisconnect(t *testing.T) {
	t.Parallel()

	releaseStop := make(chan struct{})
	server := newGracefulStopTestServer(t, http.StatusOK, releaseStop)
	worker := newGracefulStopTestWorker(t, server.URL, logger.Discard, AgentWorkerConfig{})

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
	l := logger.NewBuffer()
	worker := newGracefulStopTestWorker(t, server.URL, l, AgentWorkerConfig{})

	worker.StopGracefully()
	if err := worker.Disconnect(t.Context()); err != nil {
		t.Fatalf("Disconnect() error = %v, want nil", err)
	}

	events, _ := server.snapshot()
	if got, want := events, []string{"stop", "disconnect"}; !slices.Equal(got, want) {
		t.Errorf("request events = %v, want %v", got, want)
	}
	if !slices.ContainsFunc(l.Messages, func(message string) bool {
		return strings.Contains(message, "Failed to report graceful stop to Buildkite")
	}) {
		t.Errorf("log messages = %v, want graceful stop reporting warning", l.Messages)
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
			worker := newGracefulStopTestWorker(t, server.URL, logger.Discard, AgentWorkerConfig{})
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

func TestAgentWorker_StartReturnsWhenAcceptFailsAfterStop(t *testing.T) {
	t.Parallel()

	// Stopping the worker makes the loop that dispatched a job stop waiting
	// for its result and exit. If the accept then fails, the action loop must
	// not block forever reporting the failure to a loop that has gone.
	modes := []struct {
		mode string
		// Logged by each loop that dispatches jobs in this mode, once it has
		// stopped waiting for the result and exited.
		exits []string
		// Pings the server should see, which checks that the job arrived via
		// the loop the mode implies.
		wantPings int
	}{
		{mode: PingModePollOnly, exits: []string{"[runPingLoop] Exiting"}, wantPings: 1},
		{mode: PingModeStreamOnly, exits: []string{"[runDebouncer] Exiting"}},
		{mode: PingModeAuto, exits: []string{"[runPingLoop] Exiting", "[runDebouncer] Exiting"}},
	}
	stops := []struct {
		name string
		stop func(*AgentWorker)
	}{
		{name: "graceful stop", stop: (*AgentWorker).StopGracefully},
		{name: "ungraceful stop", stop: (*AgentWorker).StopUngracefully},
	}

	for _, mode := range modes {
		for _, stop := range stops {
			t.Run(mode.mode+"/"+stop.name, func(t *testing.T) {
				t.Parallel()

				server := NewFakeAPIServer(WithStreaming)
				t.Cleanup(server.Close)

				job := server.AddJob(nil)
				agent := server.AddAgent(gracefulStopTestSessionToken)
				server.Assign(agent, job) // the default ping handler dispatches it

				// Hold the accept until the test releases it, then reject the
				// job. A rejection isn't retried, so the action loop gets an
				// error to report straight away.
				acceptStarted := make(chan struct{})
				releaseAccept := make(chan struct{})
				var accepts atomic.Int32
				agent.AcceptHandler = func(rw http.ResponseWriter, req *http.Request) {
					if accepts.Add(1) == 1 {
						close(acceptStarted)
					}
					select {
					case <-releaseAccept:
					case <-req.Context().Done():
						return
					}
					http.Error(rw, encodeMsg("job no longer available"), http.StatusUnprocessableEntity)
				}

				// Dispatch the same job over the stream, then hold the stream
				// open so that the worker never falls back to polling.
				agent.PingStreamHandler = func(ctx context.Context, _ *connect.Request[agentedgev1.StreamPingsRequest], resp *connect.ServerStream[agentedgev1.StreamPingsResponse]) error {
					if err := resp.Send(&agentedgev1.StreamPingsResponse{
						Action: &agentedgev1.StreamPingsResponse_JobAssigned{
							JobAssigned: &agentedgev1.JobAssignedAction{
								Job: &agentedgev1.Job{Id: job.Job.ID},
							},
						},
					}); err != nil {
						return err
					}
					<-ctx.Done()
					return nil
				}

				// The loops don't expose when they exit, so scan the log for
				// their exit messages. Sends never block, so the worker can't
				// stall on logging even if a message repeats.
				exited := make(chan struct{}, len(mode.exits))
				acceptFailed := make(chan struct{}, 1)
				logReader, logWriter := io.Pipe()
				t.Cleanup(func() { _ = logWriter.Close() })
				go func() {
					for sc := bufio.NewScanner(logReader); sc.Scan(); {
						line := sc.Text()
						for _, exit := range mode.exits {
							if strings.Contains(line, exit) {
								select {
								case exited <- struct{}{}:
								default:
								}
							}
						}
						if strings.Contains(line, "failed to accept job") {
							select {
							case acceptFailed <- struct{}{}:
							default:
							}
						}
					}
				}()
				l := logger.NewConsoleLogger(logger.NewTextPrinter(logWriter), func(int) {})
				worker := newGracefulStopTestWorker(t, server.URL, l, AgentWorkerConfig{
					AgentConfiguration: AgentConfiguration{PingMode: mode.mode},
				})

				done := make(chan error, 1)
				go func() {
					done <- worker.Start(t.Context(), nil)
				}()

				waitFor := func(ch <-chan struct{}, event string) {
					t.Helper()
					select {
					case <-ch:
					case <-time.After(5 * time.Second):
						t.Fatalf("timed out waiting for %s", event)
					}
				}

				waitFor(acceptStarted, "the accept request")
				stop.stop(worker)
				for range mode.exits {
					waitFor(exited, "the loops that dispatched the job to exit")
				}
				close(releaseAccept)
				waitFor(acceptFailed, "the accept to fail")

				select {
				case err := <-done:
					if err != nil {
						t.Errorf("worker.Start() error = %v, want nil", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("worker.Start() did not return: the action loop is blocked reporting the failed accept to a loop that has exited")
				}
				if got, want := accepts.Load(), int32(1); got != want {
					t.Errorf("accept requests = %d, want %d", got, want)
				}
				if got, want := agent.Pings, mode.wantPings; got != want {
					t.Errorf("agent.Pings = %d, want %d", got, want)
				}
			})
		}
	}
}
