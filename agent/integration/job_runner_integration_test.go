package integration

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
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

	runJob(t, testRunJobConfig{
		job:           j,
		server:        server,
		agentCfg:      agent.AgentConfiguration{HooksPath: hooksDir},
		mockBootstrap: mb,
	})

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

			runJob(t, testRunJobConfig{
				job:           j,
				server:        server,
				agentCfg:      agent.AgentConfiguration{},
				mockBootstrap: mb,
			})
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

	runJob(t, testRunJobConfig{
		job:           j,
		server:        server,
		agentCfg:      agent.AgentConfiguration{},
		mockBootstrap: mb,
	})
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

	runJob(t, testRunJobConfig{
		job:           j,
		server:        server,
		agentCfg:      agent.AgentConfiguration{},
		mockBootstrap: mb,
	})
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

	runJob(t, testRunJobConfig{
		job:           j,
		server:        server,
		agentCfg:      agent.AgentConfiguration{CommandEval: true},
		mockBootstrap: mb,
	})
}
