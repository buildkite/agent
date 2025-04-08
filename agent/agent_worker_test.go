package agent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/core"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/metrics"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisconnect(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/disconnect":
			rw.WriteHeader(http.StatusOK)
			fmt.Fprintf(rw, `{"id": "fakeuuid", "connection_state": "disconnected"}`)
		default:
			t.Errorf("Unknown endpoint %s %s", req.Method, req.URL.Path)
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx := context.Background()

	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	l := logger.NewBuffer()
	client := &core.Client{
		APIClient: apiClient,
		Logger:    l,
		RetrySleepFunc: func(time.Duration) {
			t.Error("unexpected retrier sleep")
		},
	}

	worker := &AgentWorker{
		logger:             l,
		agent:              nil,
		apiClient:          apiClient,
		client:             client,
		agentConfiguration: AgentConfiguration{},
	}

	err := worker.Disconnect(ctx)
	require.NoError(t, err)

	assert.Equal(t, []string{"[info] Disconnecting...", "[info] Disconnected"}, l.Messages)
}

func TestDisconnectRetry(t *testing.T) {
	t.Parallel()

	tries := 0
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/disconnect":
			if tries < 2 { // three failures before success
				rw.WriteHeader(http.StatusInternalServerError)
				tries++
			} else {
				rw.WriteHeader(http.StatusOK)
				fmt.Fprintf(rw, `{"id": "fakeuuid", "connection_state": "disconnected"}`)
			}
		default:
			t.Errorf("Unknown endpoint %s %s", req.Method, req.URL.Path)
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx := context.Background()

	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	l := logger.NewBuffer()
	retrySleeps := make([]time.Duration, 0)
	retrySleepFunc := func(d time.Duration) {
		retrySleeps = append(retrySleeps, d)
	}
	client := &core.Client{
		APIClient:      apiClient,
		Logger:         l,
		RetrySleepFunc: retrySleepFunc,
	}

	worker := &AgentWorker{
		logger:             l,
		agent:              nil,
		apiClient:          apiClient,
		client:             client,
		agentConfiguration: AgentConfiguration{},
	}

	err := worker.Disconnect(ctx)
	assert.NoError(t, err)

	// 2 failed attempts sleep 1 second each
	assert.Equal(t, []time.Duration{1 * time.Second, 1 * time.Second}, retrySleeps)

	require.Equal(t, 4, len(l.Messages))
	assert.Equal(t, "[info] Disconnecting...", l.Messages[0])
	assert.Regexp(t, regexp.MustCompile(`\[warn\] POST http.*/disconnect: 500 Internal Server Error \(Attempt 1/4`), l.Messages[1])
	assert.Regexp(t, regexp.MustCompile(`\[warn\] POST http.*/disconnect: 500 Internal Server Error \(Attempt 2/4`), l.Messages[2])
	assert.Equal(t, "[info] Disconnected", l.Messages[3])
}

func TestAcquireJobReturnsWrappedError_WhenServerResponds422(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	jobID := "some-uuid"

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case fmt.Sprintf("/jobs/%s/acquire", jobID):
			rw.WriteHeader(http.StatusUnprocessableEntity)
			return

		default:
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	worker := &AgentWorker{
		logger:    logger.Discard,
		agent:     nil,
		apiClient: apiClient,
		client: &core.Client{
			APIClient: apiClient,
			Logger:    logger.Discard,
		},
		agentConfiguration: AgentConfiguration{},
	}

	err := worker.AcquireAndRunJob(ctx, jobID)
	if !errors.Is(err, core.ErrJobAcquisitionRejected) {
		t.Fatalf("expected worker.AcquireAndRunJob(%q) = core.ErrJobAcquisitionRejected, got %v", jobID, err)
	}
}

func TestAcquireAndRunJobWaiting(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/jobs/waitinguuid/acquire":
			if req.Header.Get("X-Buildkite-Lock-Acquire-Job") != "1" {
				http.Error(rw, "Expected X-Buildkite-Lock-Acquire-Job to be set to 1", http.StatusUnprocessableEntity)
				return
			}

			backoff_seq, err := strconv.ParseFloat(req.Header.Get("X-Buildkite-Backoff-Sequence"), 64)
			if err != nil {
				backoff_seq = 0
			}
			delay := math.Pow(2, backoff_seq)

			rw.Header().Set("Retry-After", fmt.Sprintf("%f", delay))
			rw.WriteHeader(http.StatusLocked)
			fmt.Fprintf(rw, `{"message": "Job waitinguuid is not yet eligible to be assigned"}`)
		default:
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	retrySleeps := []time.Duration{}
	retrySleepFunc := func(d time.Duration) {
		retrySleeps = append(retrySleeps, d)
	}
	client := &core.Client{
		APIClient:      apiClient,
		Logger:         logger.Discard,
		RetrySleepFunc: retrySleepFunc,
	}

	worker := &AgentWorker{
		logger:             logger.Discard,
		agent:              nil,
		apiClient:          apiClient,
		client:             client,
		agentConfiguration: AgentConfiguration{},
	}

	err := worker.AcquireAndRunJob(ctx, "waitinguuid")
	assert.ErrorContains(t, err, "423")

	if errors.Is(err, core.ErrJobAcquisitionRejected) {
		t.Fatalf("expected worker.AcquireAndRunJob(%q) not to be core.ErrJobAcquisitionRejected, but it was: %v", "waitinguuid", err)
	}

	// the last Retry-After is not recorded as the retries loop exits before using it
	expectedSleeps := make([]time.Duration, 0, 6)
	for d := 1; d <= 1<<5; d *= 2 {
		expectedSleeps = append(expectedSleeps, time.Duration(d)*time.Second)
	}
	assert.Equal(t, expectedSleeps, retrySleeps)
}

func TestAgentWorker_Start_AcquireJob_Pause_Unpause(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	server := NewFakeAPIServer()
	defer server.Close()

	job := server.AddJob(map[string]string{
		"BUILDKITE_COMMAND": "echo echo",
	})

	// Pre-register the agent.
	const agentSessionToken = "alpacas"
	agent := server.AddAgent(agentSessionToken)
	agent.PingHandler = func() (api.Ping, error) {
		switch agent.Pings {
		case 0:
			return api.Ping{
				Action:  "pause",
				Message: "Agent is now paused",
			}, nil

		case 1:
			return api.Ping{}, nil // now idle

		default:
			return api.Ping{}, errors.New("too many pings")
		}
	}

	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	l := logger.NewConsoleLogger(logger.NewTestPrinter(t), func(int) {})

	worker := NewAgentWorker(
		l,
		&api.AgentRegisterResponse{
			UUID:              uuid.New().String(),
			Name:              "agent-1",
			AccessToken:       agentSessionToken,
			Endpoint:          server.URL,
			PingInterval:      1,
			JobStatusInterval: 1,
			HeartbeatInterval: 10,
		},
		metrics.NewCollector(logger.Discard, metrics.CollectorConfig{}),
		apiClient,
		AgentWorkerConfig{
			SpawnIndex: 1,
			AgentConfiguration: AgentConfiguration{
				BootstrapScript: "./dummy_bootstrap.sh",
				BuildPath:       filepath.Join(os.TempDir(), t.Name(), "build"),
				HooksPath:       filepath.Join(os.TempDir(), t.Name(), "hooks"),
				AcquireJob:      job.Job.ID,
			},
		},
	)
	worker.noWaitBetweenPingsForTesting = true

	idleMonitor := NewIdleMonitor(1)

	if err := worker.Start(ctx, idleMonitor); err != nil {
		t.Errorf("worker.Start() = %v", err)
	}

	if got, want := agent.Pings, 2; got != want {
		t.Errorf("agent.Pings = %d, want %d", got, want)
	}
	if got, want := job.State, JobStateFinished; got != want {
		t.Errorf("job.State = %q, want %q", got, want)
	}
}

func TestAgentWorker_DisconnectAfterJob_Start_Pause_Unpause(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	server := NewFakeAPIServer()
	defer server.Close()

	job := server.AddJob(map[string]string{
		"BUILDKITE_COMMAND": "echo echo",
	})

	// Pre-register the agent.
	const agentSessionToken = "alpacas"
	agent := server.AddAgent(agentSessionToken)
	agent.PingHandler = func() (api.Ping, error) {
		switch agent.Pings {
		case 0:
			return api.Ping{
				Job: job.Job,
			}, nil

		case 1:
			return api.Ping{
				Action:  "pause",
				Message: "Agent is now paused",
			}, nil

		case 2:
			return api.Ping{}, nil // now idle

		default:
			return api.Ping{}, errors.New("too many pings")
		}
	}

	server.Assign(agent, job)

	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	l := logger.NewConsoleLogger(logger.NewTestPrinter(t), func(int) {})

	worker := NewAgentWorker(
		l,
		&api.AgentRegisterResponse{
			UUID:              uuid.New().String(),
			Name:              "agent-1",
			AccessToken:       "alpacas",
			Endpoint:          server.URL,
			PingInterval:      1,
			JobStatusInterval: 1,
			HeartbeatInterval: 10,
		},
		metrics.NewCollector(logger.Discard, metrics.CollectorConfig{}),
		apiClient,
		AgentWorkerConfig{
			SpawnIndex: 1,
			AgentConfiguration: AgentConfiguration{
				BootstrapScript:    "./dummy_bootstrap.sh",
				BuildPath:          filepath.Join(os.TempDir(), t.Name(), "build"),
				HooksPath:          filepath.Join(os.TempDir(), t.Name(), "hooks"),
				DisconnectAfterJob: true,
			},
		},
	)
	worker.noWaitBetweenPingsForTesting = true

	idleMonitor := NewIdleMonitor(1)

	if err := worker.Start(ctx, idleMonitor); err != nil {
		t.Errorf("worker.Start() = %v", err)
	}

	if got, want := agent.Pings, 3; got != want {
		t.Errorf("agent.Pings = %d, want %d", got, want)
	}
	if got, want := job.State, JobStateFinished; got != want {
		t.Errorf("job.State = %q, want %q", got, want)
	}
}

func TestAgentWorker_SetEndpointDuringRegistration(t *testing.T) {
	// The registration request is made in clicommand.AgentStartCommand, and the response
	// is passed into agent.NewAgentWorker(...), so we'll just test the response handling.
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	server := NewFakeAPIServer()
	defer server.Close()
	targetEndpoint := server.URL

	const agentSessionToken = "alpacas"
	agent := server.AddAgent(agentSessionToken)
	agent.PingHandler = func() (api.Ping, error) {
		switch agent.Pings {
		case 0:
			t.Log("server ping: disconnect")
			return api.Ping{Action: "disconnect"}, nil
		default:
			return api.Ping{}, errors.New("too many pings")
		}
	}

	// Create a listener without an HTTP server; hitting this would cause a network error/timeout.
	// This is a bit neater than using a random host/port and hoping nothing is listening on it.
	registrationServer, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("creating broken endpoint listener: %v", err)
	}
	defer registrationServer.Close()
	registrationEndpoint := fmt.Sprintf("http://%s/", registrationServer.Addr())

	// Create API client with the _old_ endpoint that it would have used for registration,
	// but that it should not connect to again.
	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: registrationEndpoint, // should not be connected to again
		Token:    "llamas",
	})

	l := logger.NewConsoleLogger(logger.NewTestPrinter(t), func(int) {})

	worker := NewAgentWorker(
		l,
		&api.AgentRegisterResponse{
			UUID:              uuid.New().String(),
			Name:              "agent-1",
			AccessToken:       agentSessionToken,
			Endpoint:          targetEndpoint, // should be used from now on
			PingInterval:      1,
			JobStatusInterval: 5,
			HeartbeatInterval: 60,
		},
		metrics.NewCollector(logger.Discard, metrics.CollectorConfig{}),
		apiClient, // at this stage, apiClient still has the old register endpoint
		AgentWorkerConfig{},
	)
	worker.noWaitBetweenPingsForTesting = true

	if err := worker.Start(ctx, NewIdleMonitor(1)); err != nil {
		t.Errorf("worker.Start() = %v", err)
	}

	if got, want := agent.Pings, 1; got != want {
		t.Errorf("agent.Pings = %d, want %d", got, want)
	}
}

func TestAgentWorker_UpdateEndpointDuringPing(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	const agentSessionToken = "alpacas"

	// the first endpoint, to be redirected from
	endpointA := NewFakeAPIServer()
	defer endpointA.Close()
	agentA := endpointA.AddAgent(agentSessionToken)

	// the second endpoint, to be redirected to
	endpointB := NewFakeAPIServer()
	defer endpointB.Close()
	agentB := endpointB.AddAgent(agentSessionToken)

	pingSequence := []string{}

	agentA.PingHandler = func() (api.Ping, error) {
		switch agentA.Pings {
		case 0:
			pingSequence = append(pingSequence, "A")
			t.Log("endpointA ping: idle")
			return api.Ping{Action: "idle"}, nil

		case 1:
			pingSequence = append(pingSequence, "A")
			endpoint := endpointB.URL
			t.Logf("endpointA ping: idle, Endpoint: %s (endpointB)", endpoint)
			return api.Ping{Action: "idle", Endpoint: endpoint}, nil

		default:
			return api.Ping{}, fmt.Errorf("endpointA unexpected ping #%d", agentA.Pings)
		}
	}

	agentB.PingHandler = func() (api.Ping, error) {
		switch agentB.Pings {
		case 0:
			pingSequence = append(pingSequence, "B")
			t.Log("endpointB ping: idle")
			return api.Ping{Action: "idle"}, nil

		case 1:
			pingSequence = append(pingSequence, "B")
			t.Log("endpointB ping: disconnect")
			return api.Ping{Action: "disconnect"}, nil

		default:
			return api.Ping{}, fmt.Errorf("endpointB unexpected ping #%d", agentB.Pings)
		}
	}

	// start on endpointA, expect to be redirected to endpointB
	endpoint := endpointA.URL

	l := logger.NewConsoleLogger(logger.NewTestPrinter(t), func(int) {})

	worker := NewAgentWorker(
		l,
		&api.AgentRegisterResponse{
			UUID:              uuid.New().String(),
			Name:              "agent-1",
			AccessToken:       "alpacas",
			Endpoint:          endpoint,
			PingInterval:      1,
			JobStatusInterval: 5,
			HeartbeatInterval: 60,
		},
		metrics.NewCollector(logger.Discard, metrics.CollectorConfig{}),
		api.NewClient(l, api.Config{
			Endpoint: endpoint,
			Token:    "llamas",
		}),
		AgentWorkerConfig{},
	)
	worker.noWaitBetweenPingsForTesting = true

	if err := worker.Start(ctx, NewIdleMonitor(1)); err != nil {
		t.Errorf("worker.Start() = %v", err)
	}

	want, got := []string{"A", "A", "B", "B"}, pingSequence
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("unexpected ping sequence (-want +got):\n%s", diff)
	}
}

func TestAgentWorker_UpdateEndpointDuringPing_FailAndRevert(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	const agentSessionToken = "alpacas"

	// A working endpoint for the original ping
	endpointA := NewFakeAPIServer()
	defer endpointA.Close()

	// A broken endpoint, to redirect the ping to
	endpointB, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("creating broken endpoint listener: %v", err)
	}
	defer endpointB.Close()

	agent := endpointA.AddAgent(agentSessionToken)
	agent.PingHandler = func() (api.Ping, error) {
		switch agent.Pings {
		case 0:
			t.Log("endpointA ping: idle")
			return api.Ping{Action: "idle"}, nil

		case 1:
			endpoint := fmt.Sprintf("http://%s/v3", endpointB.Addr().String())
			t.Logf("endpointA ping: idle, Endpoint: %s (endpointB; broken)", endpoint)
			return api.Ping{Action: "idle", Endpoint: endpoint}, nil

		case 2:
			t.Log("endpointA ping: idle")
			return api.Ping{Action: "idle"}, nil

		case 3:
			t.Log("endpointA ping: disconnect")
			return api.Ping{Action: "disconnect"}, nil

		default:
			return api.Ping{}, fmt.Errorf("endpointA unexpected ping #%d", agent.Pings)
		}
	}

	// start on endpointA, expect to be redirected to endpointB
	endpoint := endpointA.URL

	l := logger.NewConsoleLogger(logger.NewTestPrinter(t), func(int) {})

	worker := NewAgentWorker(
		l,
		&api.AgentRegisterResponse{
			UUID:              uuid.New().String(),
			Name:              "agent-1",
			AccessToken:       "alpacas",
			Endpoint:          endpoint,
			PingInterval:      1,
			JobStatusInterval: 5,
			HeartbeatInterval: 60,
		},
		metrics.NewCollector(logger.Discard, metrics.CollectorConfig{}),
		api.NewClient(l, api.Config{
			Endpoint: endpoint,
			Token:    "llamas",
			Timeout:  10 * time.Millisecond,
		}),
		AgentWorkerConfig{},
	)
	worker.noWaitBetweenPingsForTesting = true

	if err := worker.Start(ctx, NewIdleMonitor(1)); err != nil {
		t.Errorf("worker.Start() = %v", err)
	}

	if got, want := agent.Pings, 4; got != want {
		t.Errorf("agent.Pings = %d, want %d", got, want)
	}
}
