package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/metrics"
	"github.com/buildkite/bintest/v3"
)

func TestJobRunner_WhenJobHasToken_ItOverridesAccessToken(t *testing.T) {
	agentAccessToken := "llamasrock"
	jobToken := "actually-llamas-are-only-okay"

	ag := &api.AgentRegisterResponse{
		AccessToken: agentAccessToken,
	}

	j := &api.Job{
		ID:                 "my-job-id",
		ChunksMaxSizeBytes: 1024,
		Token:              jobToken,
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
		},
	}

	cfg := agent.AgentConfiguration{}

	runJob(t, ag, j, cfg, func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_AGENT_ACCESS_TOKEN"), jobToken; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_AGENT_ACCESS_TOKEN) = %q, want %q", got, want)
		}
		c.Exit(0)
	})
}

func TestJobRunnerPassesAccessTokenToJobExecute(t *testing.T) {
	ag := &api.AgentRegisterResponse{
		AccessToken: "llamasrock",
	}

	j := &api.Job{
		ID:                 "my-job-id",
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
		},
	}

	cfg := agent.AgentConfiguration{}

	runJob(t, ag, j, cfg, func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_AGENT_ACCESS_TOKEN"), "llamasrock"; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_AGENT_ACCESS_TOKEN) = %q, want %q", got, want)
		}
		c.Exit(0)
	})
}

func TestJobRunnerIgnoresPipelineChangesToProtectedVars(t *testing.T) {
	ag := &api.AgentRegisterResponse{
		AccessToken: "llamasrock",
	}

	j := &api.Job{
		ID:                 "my-job-id",
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_COMMAND":      "echo hello world",
			"BUILDKITE_COMMAND_EVAL": "false",
		},
	}

	cfg := agent.AgentConfiguration{
		CommandEval: true,
	}

	runJob(t, ag, j, cfg, func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_COMMAND_EVAL"), "true"; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_COMMAND_EVAL) = %q, want %q", got, want)
		}
		c.Exit(0)
	})

}

func runJob(t *testing.T, ag *api.AgentRegisterResponse, j *api.Job, cfg agent.AgentConfiguration, executor func(c *bintest.Call)) {
	// create a mock agent API
	server := createTestAgentEndpoint(t, "my-job-id")
	defer server.Close()

	// set up a mock executor that the runner will call
	bs, err := bintest.NewMock("buildkite-agent-job-execute")
	if err != nil {
		t.Fatalf("bintest.NewMock() error = %v", err)
	}
	defer bs.CheckAndClose(t)

	// execute the callback we have inside the executor mock
	bs.Expect().Once().AndExitWith(0).AndCallFunc(executor)

	l := logger.Discard

	// minimal metrics, this could be cleaner
	m := metrics.NewCollector(l, metrics.CollectorConfig{})
	scope := m.Scope(metrics.Tags{})

	// set the executor into the config
	cfg.JobRunScript = bs.Path

	client := api.NewClient(l, api.Config{
		Endpoint: server.URL,
		Token:    ag.AccessToken,
	})

	jr, err := agent.NewJobRunner(l, scope, ag, j, client, agent.JobRunnerConfig{
		AgentConfiguration: cfg,
	})
	if err != nil {
		t.Fatalf("agent.NewJobRunner() error = %v", err)
	}

	if err := jr.Run(context.Background()); err != nil {
		t.Errorf("jr.Run() = %v", err)
	}
}

func createTestAgentEndpoint(t *testing.T, jobID string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
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
