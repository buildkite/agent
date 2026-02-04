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
	"runtime"
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

var dummyBootstrap = "/bin/sh -c true"

func init() {
	if runtime.GOOS == "windows" {
		dummyBootstrap = "cmd.exe /c VER>NUL"
	}
}

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
				fmt.Fprintf(rw, `{"id": "fakeuuid", "connection_state": "disconnected"}`) //nolint:errcheck // The test should still fail
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

	if !errors.Is(err, core.ErrJobLocked) {
		t.Fatalf("expected worker.AcquireAndRunJob(%q) = core.ErrJobLocked, got %v", "waitinguuid", err)
	}

	// the last Retry-After is not recorded as the retries loop exits before using it
	expectedSleeps := make([]time.Duration, 0, 6)
	for d := 1; d <= 1<<5; d *= 2 {
		expectedSleeps = append(expectedSleeps, time.Duration(d)*time.Second)
	}
	assert.Equal(t, expectedSleeps, retrySleeps)
}

func TestAgentWorker_Start_AcquireJob_JobAcquisitionRejected(t *testing.T) {
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

			rw.WriteHeader(http.StatusUnprocessableEntity)
		default:
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Pre-register the agent.
	const agentSessionToken = "alpacas"

	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})

	jobToken := uuid.New().String()
	jobID := "waitinguuid"
	job := &FakeJob{
		Job: &api.Job{
			ID:                 jobID,
			Token:              jobToken,
			ChunksMaxSizeBytes: 1024,
			Env: map[string]string{
				"BUILDKITE_COMMAND": "echo echo",
			},
		},
	}

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

	// we expect the worker to try to acquire the job, but fail with ErrJobAcquisitionRejected
	// because the server returns a 422 Unprocessable Entity.
	err := worker.Start(ctx, nil)
	if !errors.Is(err, core.ErrJobAcquisitionRejected) {
		t.Fatalf("expected worker.AcquireAndRunJob(%q) = core.ErrJobAcquisitionRejected, got %v", jobID, err)
	}
}

func TestAgentWorker_Start_AcquireJob_Pause_Unpause(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	buildPath := filepath.Join(os.TempDir(), t.Name(), "build")
	hooksPath := filepath.Join(os.TempDir(), t.Name(), "hooks")
	if err := errors.Join(os.MkdirAll(buildPath, 0o777), os.MkdirAll(hooksPath, 0o777)); err != nil {
		t.Fatalf("Couldn't create directories: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(filepath.Join(os.TempDir(), t.Name())) //nolint:errcheck // Best-effort cleanup
	})

	server := NewFakeAPIServer()
	defer server.Close()

	job := server.AddJob(map[string]string{
		"BUILDKITE_COMMAND": "echo echo",
	})

	// Pre-register the agent.
	const agentSessionToken = "alpacas"
	agent := server.AddAgent(agentSessionToken)
	agent.PingHandler = func(*http.Request) (api.Ping, error) {
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
				BootstrapScript: dummyBootstrap,
				BuildPath:       buildPath,
				HooksPath:       hooksPath,
				AcquireJob:      job.Job.ID,
			},
		},
	)
	worker.noWaitBetweenPingsForTesting = true

	if err := worker.Start(ctx, nil); err != nil {
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

	buildPath := filepath.Join(os.TempDir(), t.Name(), "build")
	hooksPath := filepath.Join(os.TempDir(), t.Name(), "hooks")
	if err := errors.Join(os.MkdirAll(buildPath, 0o777), os.MkdirAll(hooksPath, 0o777)); err != nil {
		t.Fatalf("Couldn't create directories: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(filepath.Join(os.TempDir(), t.Name())) //nolint:errcheck // Best-effort cleanup
	})

	server := NewFakeAPIServer()
	defer server.Close()

	job := server.AddJob(map[string]string{
		"BUILDKITE_COMMAND": "echo echo",
	})

	// Pre-register the agent.
	const agentSessionToken = "alpacas"
	agent := server.AddAgent(agentSessionToken)
	agent.PingHandler = func(*http.Request) (api.Ping, error) {
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
				BootstrapScript:    dummyBootstrap,
				BuildPath:          buildPath,
				HooksPath:          hooksPath,
				DisconnectAfterJob: true,
			},
		},
	)
	worker.noWaitBetweenPingsForTesting = true

	if err := worker.Start(ctx, nil); err != nil {
		t.Errorf("worker.Start() = %v", err)
	}

	if got, want := agent.Pings, 3; got != want {
		t.Errorf("agent.Pings = %d, want %d", got, want)
	}
	if got, want := agent.IgnoreInDispatches, true; got != want {
		t.Errorf("agent.IgnoreInDispatches = %t, want %t", got, want)
	}
	if got, want := job.State, JobStateFinished; got != want {
		t.Errorf("job.State = %q, want %q", got, want)
	}
}

func TestAgentWorker_DisconnectAfterUptime(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	buildPath := filepath.Join(os.TempDir(), t.Name(), "build")
	hooksPath := filepath.Join(os.TempDir(), t.Name(), "hooks")
	if err := errors.Join(os.MkdirAll(buildPath, 0o777), os.MkdirAll(hooksPath, 0o777)); err != nil {
		t.Fatalf("Couldn't create directories: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(filepath.Join(os.TempDir(), t.Name())) //nolint:errcheck // Best-effort cleanup
	})

	server := NewFakeAPIServer()
	defer server.Close()

	// Create a job that the agent could potentially accept
	job := server.AddJob(map[string]string{
		"BUILDKITE_COMMAND": "echo hello",
	})

	// Pre-register the agent.
	const agentSessionToken = "alpacas"
	agent := server.AddAgent(agentSessionToken)

	pingCount := 0
	agent.PingHandler = func(*http.Request) (api.Ping, error) {
		pingCount++
		// Always offer the job to test that the agent stops accepting jobs after max lifetime
		return api.Ping{
			Job: job.Job,
		}, nil
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
				BootstrapScript:       dummyBootstrap,
				BuildPath:             buildPath,
				HooksPath:             hooksPath,
				DisconnectAfterUptime: 1 * time.Second, // max uptime
			},
		},
	)
	worker.noWaitBetweenPingsForTesting = true

	// Record start time
	startTime := time.Now()

	if err := worker.Start(ctx, nil); err != nil {
		t.Errorf("worker.Start() = %v", err)
	}

	// Check that the agent disconnected after approximately 1 second
	elapsed := time.Since(startTime)
	if elapsed < 900*time.Millisecond || elapsed > 2*time.Second {
		t.Errorf("Agent should have disconnected after ~1 second, but took %v", elapsed)
	}

	// The agent should have made at least one ping before disconnecting
	if pingCount == 0 {
		t.Error("Agent should have made at least one ping before disconnecting")
	}

	// The agent should have made at least one ping and should have disconnected
	// due to max uptime being exceeded. The important thing is that the agent
	// disconnected properly with the uptime check, which we verified above.
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
	agent.PingHandler = func(*http.Request) (api.Ping, error) {
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
	defer registrationServer.Close() //nolint:errcheck // Best-effort cleanup
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

	if err := worker.Start(ctx, nil); err != nil {
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

	agentA.PingHandler = func(*http.Request) (api.Ping, error) {
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

	agentB.PingHandler = func(*http.Request) (api.Ping, error) {
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

	if err := worker.Start(ctx, nil); err != nil {
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
	defer endpointB.Close() //nolint:errcheck // Best-effort cleanup

	agent := endpointA.AddAgent(agentSessionToken)
	agent.PingHandler = func(*http.Request) (api.Ping, error) {
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

	if err := worker.Start(ctx, nil); err != nil {
		t.Errorf("worker.Start() = %v", err)
	}

	if got, want := agent.Pings, 4; got != want {
		t.Errorf("agent.Pings = %d, want %d", got, want)
	}
}

func TestAgentWorker_SetRequestHeadersDuringRegistration(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	const headerKey = "Buildkite-Hello"
	const headerValue = "world"

	const agentSessionToken = "alpacas"
	server := NewFakeAPIServer()
	defer server.Close()
	agent := server.AddAgent(agentSessionToken)
	agent.PingHandler = func(req *http.Request) (api.Ping, error) {
		switch agent.Pings {
		case 0:
			if want, got := headerValue, req.Header.Get(headerKey); want != got {
				t.Errorf("req.Header.Get(%q) = %q, wanted %q", headerKey, got, want)
			}
			t.Log("server ping: disconnect")
			return api.Ping{Action: "disconnect"}, nil
		default:
			return api.Ping{}, fmt.Errorf("unexpected ping #%d", agent.Pings)
		}
	}
	server.AddRegistration("llamas", &api.AgentRegisterResponse{
		UUID:              uuid.New().String(),
		Name:              "agent-1",
		AccessToken:       agentSessionToken,
		PingInterval:      1,
		JobStatusInterval: 5,
		HeartbeatInterval: 60,
		RequestHeaders:    map[string]string{headerKey: headerValue},
	})

	l := logger.NewConsoleLogger(logger.NewTestPrinter(t), func(int) {})

	// The registration request is made in clicommand.AgentStartCommand, and we're not testing that
	// here, so we'll emulate what it does...
	apiClient := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamas",
	})
	client := &core.Client{APIClient: apiClient, Logger: l}
	// the underlying api.Client will capture & store the server-specified request headers here...
	reg, err := client.Register(ctx, api.AgentRegisterRequest{})
	if err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	// here we pass in the register response
	worker := NewAgentWorker(
		l,
		reg, // the AgentRegisterResponse
		metrics.NewCollector(logger.Discard, metrics.CollectorConfig{}),
		apiClient, // the api.Client which stored requestHeaders during Register
		AgentWorkerConfig{},
	)
	worker.noWaitBetweenPingsForTesting = true

	if err := worker.Start(ctx, nil); err != nil {
		t.Errorf("worker.Start() = %v", err)
	}

	if got, want := agent.Pings, 1; got != want {
		t.Errorf("agent.Pings = %d, want %d", got, want)
	}
}

func TestAgentWorker_UpdateRequestHeadersDuringPing(t *testing.T) {
	t.Parallel()

	const agentSessionToken = "alpacas"

	server := NewFakeAPIServer()
	defer server.Close()

	const headerKey = "Buildkite-Hello"
	const headerValue = "world"

	agent := server.AddAgent(agentSessionToken)
	agent.PingHandler = func(req *http.Request) (api.Ping, error) {
		switch agent.Pings {
		case 0: // no action
			if len(req.Header.Values(headerKey)) != 0 {
				t.Errorf("unexpected header: %s: %q", headerKey, req.Header.Get(headerKey))
			}
			t.Log("server ping: idle")
			return api.Ping{Action: "idle"}, nil
		case 1:
			if len(req.Header.Values(headerKey)) != 0 {
				t.Errorf("unexpected header: %s: %q", headerKey, req.Header.Get(headerKey))
			}
			t.Log("server ping: idle, set RequestHeaders")
			return api.Ping{
				Action:         "idle",
				RequestHeaders: map[string]string{headerKey: headerValue},
			}, nil
		case 2:
			if want, got := headerValue, req.Header.Get(headerKey); want != got {
				t.Errorf("req.Header.Get(%q) = %q, wanted %q", headerKey, got, want)
			}
			t.Log("server ping: idle")
			return api.Ping{Action: "idle"}, nil
		case 3:
			if want, got := headerValue, req.Header.Get(headerKey); want != got {
				t.Errorf("req.Header.Get(%q) = %q, wanted %q", headerKey, got, want)
			}
			t.Log("server ping: idle, set empty RequestHeaders")
			return api.Ping{Action: "idle", RequestHeaders: map[string]string{}}, nil
		case 4:
			if len(req.Header.Values(headerKey)) != 0 {
				t.Errorf("unexpected header: %s: %q", headerKey, req.Header.Get(headerKey))
			}
			t.Log("server ping: disconnect")
			return api.Ping{Action: "disconnect"}, nil
		default:
			return api.Ping{}, fmt.Errorf("unexpected ping #%d", agent.Pings)
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
			JobStatusInterval: 5,
			HeartbeatInterval: 60,
		},
		metrics.NewCollector(logger.Discard, metrics.CollectorConfig{}),
		apiClient,
		AgentWorkerConfig{},
	)
	worker.noWaitBetweenPingsForTesting = true

	if err := worker.Start(t.Context(), nil); err != nil {
		t.Errorf("worker.Start() = %v", err)
	}

	if got, want := agent.Pings, 5; got != want {
		t.Errorf("agent.Pings = %d, want %d", got, want)
	}
}

func TestAgentWorker_UnrecoverableErrorInPing(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	const agentSessionToken = "alpacas"

	server := NewFakeAPIServer()
	defer server.Close()

	const headerKey = "Buildkite-Hello"
	const headerValue = "world"

	agent := server.AddAgent(agentSessionToken)
	agent.PingHandler = func(req *http.Request) (api.Ping, error) {
		// Invalidate the token to trigger an unrecoverable error on
		// subsequent pings.
		server.DeleteAgent(agentSessionToken)
		return api.Ping{}, nil
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
			JobStatusInterval: 5,
			HeartbeatInterval: 60,
		},
		metrics.NewCollector(logger.Discard, metrics.CollectorConfig{}),
		apiClient,
		AgentWorkerConfig{},
	)
	worker.noWaitBetweenPingsForTesting = true

	if err := worker.Start(ctx, nil); !isUnrecoverable(err) {
		t.Errorf("worker.Start() = %v, want an unrecoverable error", err)
	}

	if got, want := agent.Pings, 1; got != want {
		t.Errorf("agent.Pings = %d, want %d", got, want)
	}
}
