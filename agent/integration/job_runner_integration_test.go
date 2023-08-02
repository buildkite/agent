package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/metrics"
	"github.com/buildkite/bintest/v3"
)

func TestPreBootstrapHookRefusesJob(t *testing.T) {
	t.Parallel()

	hooksDir, err := os.MkdirTemp("", "bootstrap-hooks")
	if err != nil {
		t.Fatalf("making bootstrap-hooks directory: %v", err)
	}

	defer os.RemoveAll(hooksDir)

	mockPB := mockPreBootstrap(t, hooksDir)
	mockPB.Expect().Once().AndCallFunc(func(c *bintest.Call) {
		c.Exit(1) // Fail the pre-bootstrap hook
	})
	defer mockPB.CheckAndClose(t)

	jobID := "my-job-id"
	j := &api.Job{
		ID:                 jobID,
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
		},
	}

	// create a mock agent API
	e := createTestAgentEndpoint()
	server := e.server("my-job-id")
	defer server.Close()

	mb := mockBootstrap(t)
	mb.Expect().NotCalled() // The bootstrap won't be called, as the pre-bootstrap hook failed
	defer mb.CheckAndClose(t)

	runJob(t, j, server, agent.AgentConfiguration{HooksPath: hooksDir}, mb)

	job := e.finishesFor(t, jobID)[0]

	if got, want := job.ExitStatus, "-1"; got != want {
		t.Errorf("job.ExitStatus = %q, want %q", got, want)
	}

	if got, want := job.SignalReason, "agent_refused"; got != want {
		t.Errorf("job.SignalReason = %q, want %q", got, want)
	}
}

func TestJobRunner_WhenBootstrapExits_ItSendsTheExitStatusToTheAPI(t *testing.T) {
	t.Parallel()

	exits := []int{0, 1, 2, 3}
	for _, exit := range exits {
		exit := exit
		t.Run(fmt.Sprintf("exit-%d", exit), func(t *testing.T) {
			t.Parallel()

			j := &api.Job{
				ID:                 "my-job-id",
				ChunksMaxSizeBytes: 1024,
				Env: map[string]string{
					"BUILDKITE_COMMAND": "echo hello world",
				},
			}

			mb := mockBootstrap(t)
			defer mb.CheckAndClose(t)

			mb.Expect().Once().AndExitWith(exit)

			e := createTestAgentEndpoint()
			server := e.server("my-job-id")
			defer server.Close()

			runJob(t, j, server, agent.AgentConfiguration{}, mb)
			finish := e.finishesFor(t, "my-job-id")[0]

			if got, want := finish.ExitStatus, strconv.Itoa(exit); got != want {
				t.Errorf("finish.ExitStatus = %q, want %q", got, want)
			}
		})
	}
}

func TestJobRunner_WhenJobHasToken_ItOverridesAccessToken(t *testing.T) {
	t.Parallel()

	jobToken := "actually-llamas-are-only-okay"

	j := &api.Job{
		ID:                 "my-job-id",
		ChunksMaxSizeBytes: 1024,
		Token:              jobToken,
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
		},
	}

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t)

	mb.Expect().Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_AGENT_ACCESS_TOKEN"), jobToken; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_AGENT_ACCESS_TOKEN) = %q, want %q", got, want)
		}
		c.Exit(0)
	})

	// create a mock agent API
	e := createTestAgentEndpoint()
	server := e.server("my-job-id")
	defer server.Close()

	runJob(t, j, server, agent.AgentConfiguration{}, mb)
}

// TODO 2023-07-17: What is this testing? How is it testing it?
// Maybe that the job runner pulls the access token from the API client? but that's all handled in the `runJob` helper...
func TestJobRunnerPassesAccessTokenToBootstrap(t *testing.T) {
	t.Parallel()

	j := &api.Job{
		ID:                 "my-job-id",
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
		},
	}

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t)

	mb.Expect().Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_AGENT_ACCESS_TOKEN"), "llamasrock"; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_AGENT_ACCESS_TOKEN) = %q, want %q", got, want)
		}
		c.Exit(0)
	})

	// create a mock agent API
	e := createTestAgentEndpoint()
	server := e.server("my-job-id")
	defer server.Close()

	runJob(t, j, server, agent.AgentConfiguration{}, mb)
}

func TestJobRunnerIgnoresPipelineChangesToProtectedVars(t *testing.T) {
	t.Parallel()

	j := &api.Job{
		ID:                 "my-job-id",
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_COMMAND":      "echo hello world",
			"BUILDKITE_COMMAND_EVAL": "false",
		},
	}

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t)

	mb.Expect().Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_COMMAND_EVAL"), "true"; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_COMMAND_EVAL) = %q, want %q", got, want)
		}
		c.Exit(0)
	})

	// create a mock agent API
	e := createTestAgentEndpoint()
	server := e.server("my-job-id")
	defer server.Close()

	runJob(t, j, server, agent.AgentConfiguration{CommandEval: true}, mb)
}

func mockBootstrap(t *testing.T) *bintest.Mock {
	t.Helper()

	// tests run using t.Run() will have a slash in their name, which will mess with paths to bintest binaries
	name := strings.ReplaceAll(t.Name(), "/", "-")
	bs, err := bintest.NewMock(fmt.Sprintf("buildkite-agent-bootstrap-%s", name))
	if err != nil {
		t.Fatalf("bintest.NewMock() error = %v", err)
	}

	return bs
}

func runJob(t *testing.T, j *api.Job, server *httptest.Server, cfg agent.AgentConfiguration, bs *bintest.Mock) {
	l := logger.Discard

	// minimal metrics, this could be cleaner
	m := metrics.NewCollector(l, metrics.CollectorConfig{})
	scope := m.Scope(metrics.Tags{})

	// set the bootstrap into the config
	cfg.BootstrapScript = bs.Path

	client := api.NewClient(l, api.Config{
		Endpoint: server.URL,
		Token:    "llamasrock",
	})

	jr, err := agent.NewJobRunner(l, client, agent.JobRunnerConfig{
		Job:                j,
		AgentConfiguration: cfg,
		MetricsScope:       scope,
	})
	if err != nil {
		t.Fatalf("agent.NewJobRunner() error = %v", err)
	}

	if err := jr.Run(context.Background()); err != nil {
		t.Errorf("jr.Run() = %v", err)
	}
}

type testAgentEndpoint struct {
	mu    sync.RWMutex
	calls map[string][][]byte
}

func createTestAgentEndpoint() *testAgentEndpoint {
	return &testAgentEndpoint{
		calls: make(map[string][][]byte, 4),
	}
}

func (tae *testAgentEndpoint) finishesFor(t *testing.T, jobID string) []api.Job {
	t.Helper()

	tae.mu.RLock()
	defer tae.mu.RUnlock()

	endpoint := fmt.Sprintf("/jobs/%s/finish", jobID)
	finishes := make([]api.Job, 0, len(tae.calls))

	for _, b := range tae.calls[endpoint] {
		var job api.Job
		err := json.Unmarshal(b, &job)
		if err != nil {
			t.Fatalf("decoding accept request body: %v", err)
		}
		finishes = append(finishes, job)
	}

	return finishes
}

func (t *testAgentEndpoint) server(jobID string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		t.mu.Lock()
		t.calls[req.URL.Path] = append(t.calls[req.URL.Path], b)
		t.mu.Unlock()

		switch req.URL.Path {
		case "/jobs/" + jobID:
			rw.WriteHeader(http.StatusOK)
			fmt.Fprintf(rw, `{"state":"running"}`)
		case "/jobs/" + jobID + "/start":
			rw.WriteHeader(http.StatusOK)
		case "/jobs/" + jobID + "/chunks":
			rw.WriteHeader(http.StatusCreated)
		case "/jobs/" + jobID + "/finish":
			rw.WriteHeader(http.StatusOK)
		default:
			http.Error(rw, fmt.Sprintf("not found; method = %q, path = %q", req.Method, req.URL.Path), http.StatusNotFound)
		}
	}))
}

func mockPreBootstrap(t *testing.T, hooksDir string) *bintest.Mock {
	t.Helper()

	mock, err := bintest.NewMock(fmt.Sprintf("buildkite-agent-pre-bootstrap-hook-%s", t.Name()))
	if err != nil {
		t.Fatalf("bintest.NewMock() error = %v", err)
	}

	hookScript := filepath.Join(hooksDir, "pre-bootstrap")
	body := ""

	if runtime.GOOS == "windows" {
		// You may be tempted to change this to `@%q`, but please do not. bintest doesn't like it when things change.
		// (%q escapes backslashes, which are windows path separators and leads to this test failing on windows)
		body = fmt.Sprintf(`@"%s"`, mock.Path)
		hookScript += ".bat"
	} else {
		body = "#!/bin/sh\n" + mock.Path
	}

	if err := os.MkdirAll(hooksDir, 0o700); err != nil {
		t.Fatalf("creating pre-bootstrap hook mock: os.MkdirAll() error = %v", err)
	}

	if err := os.WriteFile(hookScript, []byte(body), 0o777); err != nil {
		t.Fatalf("creating pre-bootstrap hook mock: s.WriteFile() error = %v", err)
	}

	return mock
}
