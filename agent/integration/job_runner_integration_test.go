package integration

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/agent"
	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/bintest/v3"
)

func TestPreBootstrapHookScripts(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name     string
		contents string
		ext      string
		allowed  bool
	}

	testCases := []testCase{
		{
			name:     "sh_success",
			contents: "#!/bin/sh\necho hello from a pre-bootstrap hook!\n",
			ext:      "",
			allowed:  true,
		},
		{
			name:     "sh_failure",
			contents: "#!/bin/sh\nexit 1\n",
			ext:      "",
			allowed:  false,
		},
	}

	if runtime.GOOS == "windows" {
		testCases = append(
			testCases,
			testCase{
				name:     "bat_success",
				contents: "exit 0",
				ext:      ".bat",
				allowed:  true,
			},
			testCase{
				name:     "bat_failure",
				contents: "exit 1",
				ext:      ".bat",
				allowed:  false,
			},
			testCase{
				name:     "powershell_failure",
				contents: "Exit 0",
				ext:      ".ps1",
				allowed:  true,
			},
			testCase{
				name:     "powershell_failure",
				contents: "Exit 1",
				ext:      ".ps1",
				allowed:  false,
			},
		)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			hooksDir, err := os.MkdirTemp("", "bootstrap-hooks")
			if err != nil {
				t.Fatalf("making bootstrap-hooks directory: %v", err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(hooksDir) })

			hookPath := filepath.Join(hooksDir, "pre-bootstrap"+tc.ext)
			testMainPath, err := os.Executable()
			if err != nil {
				t.Fatalf("os.Executable() error = %v, want nil", err)
			}

			// Write pre-bootstrap hook in a subprocess to avoid intermittent ETXTBSY errors on Linux
			cmd := exec.Command(testMainPath, "write-exec", hookPath)
			cmd.Stdin = strings.NewReader(tc.contents)
			err = cmd.Run()
			if err != nil {
				t.Fatalf("cmd.Run() error = %v, want nil", err)
			}

			// Creates a mock agent API
			e := createTestAgentEndpoint()
			server := e.server()
			t.Cleanup(server.Close)

			j := &api.Job{
				ID:                 defaultJobID,
				ChunksMaxSizeBytes: 1024,
				Env: map[string]string{
					"BUILDKITE_COMMAND": "echo hello world",
				},
				Token: "bkaj_job-token",
			}

			mb := mockBootstrap(t)
			if tc.allowed {
				mb.Expect().Once().AndExitWith(0)
			} else {
				mb.Expect().NotCalled()
			}
			err = runJob(t, ctx, testRunJobConfig{
				job:           j,
				server:        server,
				agentCfg:      agent.AgentConfiguration{HooksPath: hooksDir},
				mockBootstrap: mb,
			})
			if err != nil {
				t.Fatalf("runJob() error = %v", err)
			}

			mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t
		})
	}
}

func TestPreBootstrapHookRefusesJob(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	hooksDir, err := os.MkdirTemp("", "bootstrap-hooks")
	if err != nil {
		t.Fatalf("making bootstrap-hooks directory: %v", err)
	}

	t.Cleanup(func() {
		os.RemoveAll(hooksDir) //nolint:errcheck // Hooks dir removal is best-effort cleanup.
	})

	mockPB := mockPreBootstrap(t, hooksDir)
	mockPB.Expect().Once().AndCallFunc(func(c *bintest.Call) {
		c.Exit(1) // Fail the pre-bootstrap hook
	})
	defer mockPB.CheckAndClose(t) //nolint:errcheck // bintest logs to t

	jobID := "my-job-id"
	j := &api.Job{
		ID:                 jobID,
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
		},
		Token: "bkaj_job-token",
	}

	// create a mock agent API
	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	mb := mockBootstrap(t)
	mb.Expect().NotCalled()   // The bootstrap won't be called, as the pre-bootstrap hook failed
	defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t

	err = runJob(t, ctx, testRunJobConfig{
		job:           j,
		server:        server,
		agentCfg:      agent.AgentConfiguration{HooksPath: hooksDir},
		mockBootstrap: mb,
	})
	if err != nil {
		t.Fatalf("runJob() error = %v", err)
	}

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
	ctx := context.Background()

	exits := []int{0, 1, 2, 3}
	for _, exit := range exits {
		t.Run(fmt.Sprintf("exit-%d", exit), func(t *testing.T) {
			t.Parallel()

			j := &api.Job{
				ID:                 "my-job-id",
				ChunksMaxSizeBytes: 1024,
				Env: map[string]string{
					"BUILDKITE_COMMAND": "echo hello world",
				},
				Token: "bkaj_job-token",
			}

			mb := mockBootstrap(t)
			defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t

			mb.Expect().Once().AndExitWith(exit)

			e := createTestAgentEndpoint()
			server := e.server()
			defer server.Close()

			err := runJob(t, ctx, testRunJobConfig{
				job:           j,
				server:        server,
				agentCfg:      agent.AgentConfiguration{},
				mockBootstrap: mb,
			})
			if err != nil {
				t.Fatalf("runJob() error = %v", err)
			}

			finish := e.finishesFor(t, "my-job-id")[0]

			if got, want := finish.ExitStatus, strconv.Itoa(exit); got != want {
				t.Errorf("finish.ExitStatus = %q, want %q", got, want)
			}
		})
	}
}

func TestJobRunner_WhenJobHasToken_ItOverridesAccessToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	jobToken := "bkaj_actually-llamas-are-only-okay"

	j := &api.Job{
		ID:                 "my-job-id",
		ChunksMaxSizeBytes: 1024,
		Token:              jobToken,
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
		},
	}

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t

	mb.Expect().Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_AGENT_ACCESS_TOKEN"), jobToken; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_AGENT_ACCESS_TOKEN) = %q, want %q", got, want)
		}
		c.Exit(0)
	})

	// create a mock agent API
	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	err := runJob(t, ctx, testRunJobConfig{
		job:           j,
		server:        server,
		agentCfg:      agent.AgentConfiguration{},
		mockBootstrap: mb,
	})
	if err != nil {
		t.Fatalf("runJob() error = %v", err)
	}
}

// TODO 2023-07-17: What is this testing? How is it testing it?
// Maybe that the job runner pulls the access token from the API client? but that's all handled in the `runJob` helper...
func TestJobRunnerPassesAccessTokenToBootstrap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	j := &api.Job{
		ID:                 "my-job-id",
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
		},
		Token: "bkaj_job-token",
	}

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t

	mb.Expect().Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_AGENT_ACCESS_TOKEN"), "bkaj_job-token"; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_AGENT_ACCESS_TOKEN) = %q, want %q", got, want)
		}
		c.Exit(0)
	})

	// create a mock agent API
	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	err := runJob(t, ctx, testRunJobConfig{
		job:           j,
		server:        server,
		agentCfg:      agent.AgentConfiguration{},
		mockBootstrap: mb,
	})
	if err != nil {
		t.Fatalf("runJob() error = %v", err)
	}
}

func TestJobRunnerIgnoresPipelineChangesToProtectedVars(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	j := &api.Job{
		ID:                 "my-job-id",
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_COMMAND":      "echo hello world",
			"BUILDKITE_COMMAND_EVAL": "false",
		},
		Token: "bkaj_job-token",
	}

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t

	mb.Expect().Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_COMMAND_EVAL"), "true"; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_COMMAND_EVAL) = %q, want %q", got, want)
		}
		c.Exit(0)
	})

	// create a mock agent API
	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	testCfg := testRunJobConfig{
		job:           j,
		server:        server,
		agentCfg:      agent.AgentConfiguration{CommandEval: true},
		mockBootstrap: mb,
	}

	if err := runJob(t, ctx, testCfg); err != nil {
		t.Errorf("runJob(t, ctx, %v) = %v", testCfg, err)
	}
}

func TestChunksIntervalSeconds_ControlsUploadTiming(t *testing.T) {
	t.Parallel()

	runTestWithInterval := func(t *testing.T, intervalSeconds int) int {
		t.Helper()

		var chunkCount atomic.Int64

		e := createTestAgentEndpoint()
		server := e.server(route{
			Method: "POST",
			Path:   "/jobs/{id}/chunks",
			HandlerFunc: func(rw http.ResponseWriter, req *http.Request) {
				chunkCount.Add(1)
				e.chunksHandler()(rw, req)
			},
		})
		t.Cleanup(server.Close)

		j := &api.Job{
			ID:                    defaultJobID,
			ChunksMaxSizeBytes:    100_000, // large number that will never get divided into multiple chunks
			ChunksIntervalSeconds: intervalSeconds,
			Env:                   map[string]string{},
			Token:                 "bkaj_job-token",
		}

		mb := mockBootstrap(t)
		mb.Expect().Once().AndCallFunc(func(c *bintest.Call) {
			start := time.Now()
			for time.Since(start) < 4*time.Second {
				_, _ = fmt.Fprintf(c.Stdout, "Log output at start+%v\n", time.Since(start))
				time.Sleep(100 * time.Millisecond)
			}
			c.Exit(0)
		})

		if err := runJob(t, t.Context(), testRunJobConfig{
			job:           j,
			server:        server,
			agentCfg:      agent.AgentConfiguration{},
			mockBootstrap: mb,
		}); err != nil {
			t.Fatalf("runJob() error = %v", err)
		}

		mb.CheckAndClose(t) //nolint:errcheck

		t.Logf("Interval %ds: %d chunks uploaded", intervalSeconds, chunkCount.Load())
		return int(chunkCount.Load())
	}

	t.Run("1s interval", func(t *testing.T) {
		t.Parallel()

		// With a 4s job:
		// 1s interval:
		//   first chunk + (chunks at +1s, +2s, +3s) + final chunk = 5 chunks
		// Except if the first chunk is made at 1s, in which case there could be
		// only 4 chunks (at around 1s, 2s, 3s, and a final chunk after 4s).
		if got, want := runTestWithInterval(t, 1), []int{4, 5}; !slices.Contains(want, got) {
			t.Errorf("runTestWithInterval(t, 1) = %d chunks, want one of %v", got, want)
		}
	})

	t.Run("2s interval", func(t *testing.T) {
		t.Parallel()

		// With a 4s job:
		// 2s interval:
		//   first chunk + chunk at +2s + final chunk around 4s = 3 chunks
		// Except if the first chunk is made at 2s, in which case there could be
		// only 2 chunks (at around 2s and a final chunk after 4s).
		if got, want := runTestWithInterval(t, 2), []int{2, 3}; !slices.Contains(want, got) {
			t.Errorf("runTestWithInterval(t, 2) = %d chunks, want one of %v", got, want)
		}
	})
}
