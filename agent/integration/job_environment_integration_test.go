package integration

import (
	"context"
	"testing"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
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
	defer mb.CheckAndClose(t)

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
