package integration

import (
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

func TestJobRunnerPassesAccessTokenToBootstrap(t *testing.T) {
	ag := &api.AgentRegisterResponse{
		AccessToken: "llamasrock",
	}

	j := &api.Job{
		ID:                 `my-job-id`,
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			`BUILDKITE_COMMAND`: `echo hello world`,
		},
	}

	cfg := agent.AgentConfiguration{}

	runJob(t, ag, j, cfg, func(c *bintest.Call) {
		if c.GetEnv("BUILDKITE_AGENT_ACCESS_TOKEN") != `llamasrock` {
			t.Errorf("Expected access token to be %q, got %q\n",
				`llamasrock`, c.GetEnv("BUILDKITE_AGENT_ACCESS_TOKEN"))
		}
		c.Exit(0)
	})
}

func TestJobRunnerIgnoresPipelineChangesToProtectedVars(t *testing.T) {
	ag := &api.AgentRegisterResponse{
		AccessToken: "llamasrock",
	}

	j := &api.Job{
		ID:                 `my-job-id`,
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			`BUILDKITE_COMMAND`:      `echo hello world`,
			`BUILDKITE_COMMAND_EVAL`: `false`,
		},
	}

	cfg := agent.AgentConfiguration{
		CommandEval: true,
	}

	runJob(t, ag, j, cfg, func(c *bintest.Call) {
		if c.GetEnv("BUILDKITE_COMMAND_EVAL") != `true` {
			t.Errorf("Expected BUILDKITE_COMMAND_EVAL to be %q, got %q\n",
				`true`, c.GetEnv("BUILDKITE_COMMAND_EVAL"))
		}
		c.Exit(0)
	})

}

func runJob(t *testing.T, ag *api.AgentRegisterResponse, j *api.Job, cfg agent.AgentConfiguration, bootstrap func(c *bintest.Call)) {
	// create a mock agent API
	server := createTestAgentEndpoint(t, `my-job-id`)
	defer server.Close()

	// set up a mock bootstrap that the runner will call
	bs, err := bintest.NewMock("buildkite-agent-bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	defer bs.CheckAndClose(t)

	// execute the callback we have inside the bootstrap mock
	bs.Expect().Once().AndExitWith(0).AndCallFunc(bootstrap)

	l := logger.Discard

	// minimal metrics, this could be cleaner
	m := metrics.NewCollector(l, metrics.CollectorConfig{})
	scope := m.Scope(metrics.Tags{})

	// set the bootstrap into the config
	cfg.BootstrapScript = bs.Path

	client := api.NewClient(l, api.Config{
		Endpoint: server.URL,
		Token:    ag.AccessToken,
	})

	jr, err := agent.NewJobRunner(l, scope, ag, j, client, agent.JobRunnerConfig{
		AgentConfiguration: cfg,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err = jr.Run(); err != nil {
		t.Fatal(err)
	}
}

func createTestAgentEndpoint(t *testing.T, jobID string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case `/jobs/` + jobID:
			rw.WriteHeader(http.StatusOK)
			fmt.Fprintf(rw, `{"state":"running"}`)
		case `/jobs/` + jobID + `/start`:
			rw.WriteHeader(http.StatusOK)
		case `/jobs/` + jobID + `/chunks`:
			rw.WriteHeader(http.StatusCreated)
		case `/jobs/` + jobID + `/finish`:
			rw.WriteHeader(http.StatusOK)
		default:
			t.Errorf("Unknown endpoint %s %s", req.Method, req.URL.Path)
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))
}
