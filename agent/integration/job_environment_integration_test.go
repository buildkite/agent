package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/agent"
	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
	"github.com/buildkite/bintest/v3"
	"github.com/buildkite/go-pipeline"
)

func TestWhenCachePathsSetInJobStep_CachePathsEnvVarIsSet(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	job := &api.Job{
		ID:                 "my-job-id",
		ChunksMaxSizeBytes: 1024,
		Step: pipeline.CommandStep{
			Cache: &pipeline.Cache{
				Paths: []string{"foo", "bar"},
			},
		},
		Token: "bkaj_job-token",
	}

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t

	mb.Expect().Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_AGENT_CACHE_PATHS"), "foo,bar"; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_AGENT_CACHE_PATHS) = %q, want %q", got, want)
		}
		c.Exit(0)
	})

	// create a mock agent API
	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	err := runJob(t, ctx, testRunJobConfig{
		job:           job,
		server:        server,
		agentCfg:      agent.AgentConfiguration{},
		mockBootstrap: mb,
	})
	if err != nil {
		t.Fatalf("runJob() error = %v", err)
	}
}

func TestCacheSettingsOnSelfHosted_LogsMessage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	jobID := "cache-self-hosted-job"
	job := &api.Job{
		ID:                 jobID,
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_COMPUTE_TYPE": "self-hosted",
		},
		Step: pipeline.CommandStep{
			Cache: &pipeline.Cache{
				Paths: []string{"vendor", "node_modules"},
			},
		},
		Token: "bkaj_job-token",
	}

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t
	mb.Expect().Once().AndExitWith(0)

	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	err := runJob(t, ctx, testRunJobConfig{
		job:           job,
		server:        server,
		agentCfg:      agent.AgentConfiguration{},
		mockBootstrap: mb,
	})
	if err != nil {
		t.Fatalf("runJob() error = %v", err)
	}

	logs := e.logsFor(t, jobID)
	if !strings.Contains(logs, "Cache settings detected on self-hosted agent") {
		t.Errorf("expected logs to contain cache warning for self-hosted agent, got %q", logs)
	}
	if !strings.Contains(logs, "vendor, node_modules") {
		t.Errorf("expected logs to contain cache paths, got %q", logs)
	}
}

func TestCacheSettingsOnHosted_DoesNotLogMessage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	jobID := "cache-hosted-job"
	job := &api.Job{
		ID:                 jobID,
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_COMPUTE_TYPE": "hosted",
		},
		Step: pipeline.CommandStep{
			Cache: &pipeline.Cache{
				Paths: []string{"vendor", "node_modules"},
			},
		},
		Token: "bkaj_job-token",
	}

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t
	mb.Expect().Once().AndExitWith(0)

	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	err := runJob(t, ctx, testRunJobConfig{
		job:           job,
		server:        server,
		agentCfg:      agent.AgentConfiguration{},
		mockBootstrap: mb,
	})
	if err != nil {
		t.Fatalf("runJob() error = %v", err)
	}

	logs := e.logsFor(t, jobID)
	if strings.Contains(logs, "Cache settings detected on self-hosted agent") {
		t.Errorf("expected logs to NOT contain cache warning for hosted agent, got %q", logs)
	}
}

func TestNoCacheSettings_DoesNotLogMessage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	jobID := "no-cache-job"
	job := &api.Job{
		ID:                 jobID,
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_COMPUTE_TYPE": "self-hosted",
		},
		Step:  pipeline.CommandStep{},
		Token: "bkaj_job-token",
	}

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t
	mb.Expect().Once().AndExitWith(0)

	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	err := runJob(t, ctx, testRunJobConfig{
		job:           job,
		server:        server,
		agentCfg:      agent.AgentConfiguration{},
		mockBootstrap: mb,
	})
	if err != nil {
		t.Fatalf("runJob() error = %v", err)
	}

	logs := e.logsFor(t, jobID)
	if strings.Contains(logs, "Cache settings detected on self-hosted agent") {
		t.Errorf("expected logs to NOT contain cache warning when no cache settings, got %q", logs)
	}
}

func TestBuildkiteRequestHeaders(t *testing.T) {
	t.Parallel()

	// create a mock agent API
	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	// create a client with server-specified headers
	l := logger.NewConsoleLogger(logger.NewTestPrinter(t), func(int) {})
	client := api.NewClient(l, api.Config{
		Endpoint:  server.URL,
		Token:     "llamasrock",
		DebugHTTP: true,
	})
	headers := client.ServerSpecifiedRequestHeaders()
	// That getter isn't designed to modify the headers, but all's fair in test setup code and war.
	headers.Set("Buildkite-Hello", "world")

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t

	// The main assertion: that the `Buildkite-Hello: world` server-specified request header is
	// passed to the job environment as BUILDKITE_REQUEST_HEADER_BUILDKITE_HELLO=world. From there,
	// it'll be picked up by api.NewClient() in sub-processes like `buildkite-agent annotate` etc.
	mb.Expect().Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_REQUEST_HEADER_BUILDKITE_HELLO"), "world"; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_REQUEST_HEADER_BUILDKITE_HELLO) = %q, want %q", got, want)
		}
		c.Exit(0)
	})

	err := runJob(t, context.Background(), testRunJobConfig{
		job: &api.Job{
			ID:                 "00000000-0000-0000-0000-000000000123",
			ChunksMaxSizeBytes: 1024,
			Step:               pipeline.CommandStep{},
			Token:              "bkaj_job-token",
		},
		server:        server,
		agentCfg:      agent.AgentConfiguration{},
		mockBootstrap: mb,
		client:        client,
	})
	if err != nil {
		t.Fatalf("runJob() error = %v", err)
	}
}
