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
	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/logger"
	"github.com/buildkite/agent/v4/metrics"
	"github.com/buildkite/bintest/v3"
)

func TestPostBootstrapHook(t *testing.T) {
	t.Parallel()
	ctx, _ := experiments.Enable(t.Context(), experiments.PostBootstrapHook)
	for _, tc := range []struct {
		name          string
		bootstrapExit int
		hookExit      int
		startFailure  bool
	}{
		{name: "success"},
		{name: "failed_bootstrap", bootstrapExit: 7},
		{name: "failed_hook", hookExit: 3},
		{name: "start_failure", bootstrapExit: -1, startFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hooksDir := t.TempDir()
			e := createTestAgentEndpoint()
			server := e.server()
			defer server.Close()
			post := mockAgentHook(t, hooksDir, "post-bootstrap")
			defer post.CheckAndClose(t) //nolint:errcheck // bintest logs to t
			post.Expect().Once().AndCallFunc(func(c *bintest.Call) {
				for key, want := range map[string]string{
					"BUILDKITE_JOB_ID":                  defaultJobID,
					"BUILDKITE_AGENT_ACCESS_TOKEN":      "bkaj_job-token",
					"BUILDKITE_AGENT_ENDPOINT":          server.URL,
					"BUILDKITE_NO_HTTP2":                "false",
					"BUILDKITE_AGENT_DEBUG":             "false",
					"BUILDKITE_AGENT_DEBUG_HTTP":        "false",
					"BUILDKITE_BOOTSTRAP_EXIT_STATUS":   strconv.Itoa(tc.bootstrapExit),
					"BUILDKITE_BOOTSTRAP_SIGNAL":        "",
					"BUILDKITE_BOOTSTRAP_SIGNAL_REASON": "",
					"POST_BOOTSTRAP_UNTRUSTED":          "",
				} {
					if tc.startFailure && key == "BUILDKITE_BOOTSTRAP_SIGNAL_REASON" {
						want = agent.SignalReasonProcessRunError
					}
					if got := c.GetEnv(key); got != want {
						t.Errorf("%s = %q, want %q", key, got, want)
					}
				}
				for _, key := range []string{"BUILDKITE_ENV_FILE", "BUILDKITE_ENV_JSON_FILE"} {
					if contents, err := os.ReadFile(c.GetEnv(key)); err != nil || !strings.Contains(string(contents), "POST_BOOTSTRAP_UNTRUSTED") {
						t.Errorf("%s must still contain the job environment: %v", key, err)
					}
				}
				if finishes := e.finishesFor(t, defaultJobID); len(finishes) != 0 {
					t.Error("job finished before post-bootstrap completed")
				}
				wantLog := "final bootstrap output"
				if tc.startFailure {
					wantLog = "Error running job:"
				}
				if !strings.Contains(e.logsFor(t, defaultJobID), wantLog) {
					t.Errorf("job logs must contain %q before post-bootstrap starts", wantLog)
				}
				if _, err := fmt.Fprintln(c.Stdout, "host cleanup output"); err != nil {
					t.Errorf("writing hook output: %v", err)
				}
				c.Exit(tc.hookExit)
			})
			mb := mockBootstrap(t)
			defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t
			logs := logger.NewBuffer()
			cfg := testRunJobConfig{
				job: &api.Job{
					ID: defaultJobID, Token: "bkaj_job-token", ChunksMaxSizeBytes: 1024,
					ChunksIntervalSeconds: 3600, // Keep final output buffered until the process exits.
					Env:                   map[string]string{"POST_BOOTSTRAP_UNTRUSTED": "must not be exported"},
				},
				server: server, agentCfg: agent.AgentConfiguration{HooksPath: hooksDir}, logger: logs,
			}
			if tc.startFailure {
				mb.Expect().NotCalled()
				cfg.agentCfg.BootstrapScript = filepath.Join(t.TempDir(), "missing-bootstrap")
			} else {
				mb.Expect().Once().AndCallFunc(func(c *bintest.Call) {
					if _, err := fmt.Fprintln(c.Stdout, "final bootstrap output"); err != nil {
						t.Errorf("writing bootstrap output: %v", err)
					}
					c.Exit(tc.bootstrapExit)
				})
				cfg.mockBootstrap = mb
			}
			if err := runJob(t, ctx, cfg); err != nil {
				t.Fatalf("runJob: %v", err)
			}
			finish := e.finishesFor(t, defaultJobID)[0]
			if finish.ExitStatus != strconv.Itoa(tc.bootstrapExit) {
				t.Errorf("exit status = %s, want %d", finish.ExitStatus, tc.bootstrapExit)
			}
			if strings.Contains(e.logsFor(t, defaultJobID), "host cleanup output") {
				t.Error("post-bootstrap output leaked into job logs")
			}
			if !strings.Contains(strings.Join(logs.Messages, "\n"), "host cleanup output") {
				t.Error("post-bootstrap output missing from agent logs")
			}
			if tc.hookExit != 0 && !strings.Contains(strings.Join(logs.Messages, "\n"), "[error] Finished post-bootstrap hook") {
				t.Error("post-bootstrap failure missing from agent logs")
			}
		})
	}
}

func TestPostBootstrapHookRequiresExperiment(t *testing.T) {
	t.Parallel()
	hooksDir := t.TempDir()
	post := mockAgentHook(t, hooksDir, "post-bootstrap")
	post.Expect().NotCalled()
	defer post.CheckAndClose(t) //nolint:errcheck // bintest logs to t
	mb := mockBootstrap(t)
	mb.Expect().Once().AndExitWith(7)
	defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t
	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()
	if err := runJob(t, t.Context(), testRunJobConfig{
		job: &api.Job{
			ID: defaultJobID, Token: "bkaj_job-token", ChunksMaxSizeBytes: 1024,
			Env: map[string]string{"BUILDKITE_AGENT_EXPERIMENT": experiments.PostBootstrapHook},
		},
		server: server, mockBootstrap: mb,
		agentCfg: agent.AgentConfiguration{HooksPath: hooksDir, PostBootstrapTimeout: time.Second},
	}); err != nil {
		t.Fatal(err)
	}
	if finish := e.finishesFor(t, defaultJobID)[0]; finish.ExitStatus != "7" {
		t.Errorf("job exit status = %s, want 7", finish.ExitStatus)
	}
}

func TestPostBootstrapHookTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell and signals")
	}
	t.Parallel()
	ctx, _ := experiments.Enable(t.Context(), experiments.PostBootstrapHook)
	for _, tc := range []struct {
		name     string
		timeout  time.Duration
		timedOut bool
	}{
		{name: "default_one_second", timedOut: true},
		{name: "shorter_override", timeout: 50 * time.Millisecond, timedOut: true},
		{name: "longer_override", timeout: 3 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hooksDir := t.TempDir()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(executable, "write-exec", filepath.Join(hooksDir, "post-bootstrap"))
			// Both the hook and its child ignore SIGTERM. The timeout must kill
			// their process group without another cancellation grace period.
			cmd.Stdin = strings.NewReader("#!/bin/sh\ntrap '' TERM\n(sleep 2; echo cleanup-finished) &\nwait\n")
			if err := cmd.Run(); err != nil {
				t.Fatal(err)
			}
			e := createTestAgentEndpoint()
			server := e.server()
			defer server.Close()
			mb := mockBootstrap(t)
			defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t
			mb.Expect().Once().AndExitWith(7)
			logs := logger.NewBuffer()
			if err := runJob(t, ctx, testRunJobConfig{
				job: &api.Job{
					ID: defaultJobID, Token: "bkaj_job-token", ChunksMaxSizeBytes: 1024,
					Env: map[string]string{"BUILDKITE_POST_BOOTSTRAP_TIMEOUT": "30s"},
				},
				server: server, mockBootstrap: mb, logger: logs,
				agentCfg: agent.AgentConfiguration{
					HooksPath: hooksDir, PostBootstrapTimeout: tc.timeout,
					CancelSignalTimeout: 30 * time.Second,
				},
			}); err != nil {
				t.Fatal(err)
			}
			output := strings.Join(logs.Messages, "\n")
			if got := strings.Contains(output, "post-bootstrap hook timed out"); got != tc.timedOut {
				t.Errorf("timeout warning present = %t, want %t", got, tc.timedOut)
			}
			if tc.timeout == 0 && !strings.Contains(output, "post-bootstrap hook timed out after 1s") {
				t.Error("unconfigured hook must use the one-second timeout")
			}
			if got := strings.Contains(output, "cleanup-finished"); got == tc.timedOut {
				t.Errorf("cleanup completed = %t, want %t", got, !tc.timedOut)
			}
			finish := e.finishesFor(t, defaultJobID)[0]
			if finish.ExitStatus != "7" || finish.Signal != "" || finish.SignalReason != "" {
				t.Errorf("job result = %+v, want exit 7 without a signal or signal reason", finish)
			}
		})
	}
}

func TestPostBootstrapHookAfterSIGKILL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX signals")
	}
	t.Parallel()
	experimentCtx, _ := experiments.Enable(t.Context(), experiments.PostBootstrapHook)
	for _, tc := range []struct {
		name          string
		reason        agent.CancelReason
		cancelContext bool
	}{
		{name: "cancel", reason: agent.CancelReasonJobState},
		{name: "timeout", reason: agent.CancelReasonJobTimeout},
		{name: "agent_stop", reason: agent.CancelReasonAgentStopping},
		{name: "cancelled_context", cancelContext: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(experimentCtx)
			defer cancel()
			hooksDir := t.TempDir()
			ready := make(chan struct{})
			mb := mockBootstrap(t)
			defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t
			mb.Expect().Once().AndCallFunc(func(c *bintest.Call) {
				c.Exit(0)
				close(ready)
			})
			// Ignore SIGTERM so cancellation must escalate to SIGKILL. The
			// readiness command runs only once the signal disposition is set.
			bootstrap := filepath.Join(t.TempDir(), "bootstrap")
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(executable, "write-exec", bootstrap)
			cmd.Stdin = strings.NewReader(fmt.Sprintf("#!/bin/sh\ntrap '' TERM\n%q\nwhile :; do sleep 1; done\n", mb.Path))
			if err := cmd.Run(); err != nil {
				t.Fatal(err)
			}
			e := createTestAgentEndpoint()
			server := e.server()
			defer server.Close()
			post := mockAgentHook(t, hooksDir, "post-bootstrap")
			defer post.CheckAndClose(t) //nolint:errcheck // bintest logs to t
			post.Expect().Once().AndCallFunc(func(c *bintest.Call) {
				if got := c.GetEnv("BUILDKITE_BOOTSTRAP_SIGNAL"); got != "SIGKILL" {
					t.Errorf("signal = %q, want SIGKILL", got)
				}
				if got := c.GetEnv("BUILDKITE_BOOTSTRAP_EXIT_STATUS"); got != "-1" {
					t.Errorf("exit status = %q, want -1", got)
				}
				if !tc.cancelContext {
					want := agent.SignalReasonCancel
					if tc.reason == agent.CancelReasonAgentStopping {
						want = agent.SignalReasonAgentStop
					}
					if got := c.GetEnv("BUILDKITE_BOOTSTRAP_SIGNAL_REASON"); got != want {
						t.Errorf("signal reason = %q, want %q", got, want)
					}
				}
				if len(e.finishesFor(t, defaultJobID)) != 0 {
					t.Error("job finished before cleanup")
				}
				c.Exit(0)
			})
			m := metrics.NewCollector(logger.Discard, metrics.CollectorConfig{})
			jr, err := agent.NewJobRunner(ctx, logger.Discard, api.NewClient(logger.Discard, api.Config{Endpoint: server.URL}), agent.JobRunnerConfig{
				Job:                &api.Job{ID: defaultJobID, Token: "bkaj_job-token", ChunksMaxSizeBytes: 1024},
				AgentConfiguration: agent.AgentConfiguration{HooksPath: hooksDir, BootstrapScript: bootstrap, CancelSignalTimeout: 10 * time.Millisecond},
				MetricsScope:       m.Scope(metrics.Tags{}),
				JobStatusInterval:  time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}
			cancelDone := make(chan struct{})
			go func() {
				defer close(cancelDone)
				select {
				case <-ready:
				case <-ctx.Done():
					return
				}
				if tc.cancelContext {
					cancel()
				} else if err := jr.Cancel(tc.reason); err != nil {
					t.Errorf("Cancel: %v", err)
				}
			}()
			if err := jr.Run(ctx, nil); err != nil {
				t.Errorf("Run: %v", err)
			}
			<-cancelDone
			if !tc.cancelContext {
				finish := e.finishesFor(t, defaultJobID)[0]
				if finish.ExitStatus != "-1" || finish.Signal != "SIGKILL" {
					t.Errorf("finish = %+v, want SIGKILL exit -1", finish)
				}
			}
		})
	}
}

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
			ctx := t.Context()

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
	ctx, _ := experiments.Enable(t.Context(), experiments.PostBootstrapHook)

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
	post := mockAgentHook(t, hooksDir, "post-bootstrap")
	post.Expect().NotCalled()
	defer post.CheckAndClose(t) //nolint:errcheck // bintest logs to t

	jobID := "my-job-id"
	j := &api.Job{
		ID:                 jobID,
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_COMMAND":              "echo hello world",
			"BUILDKITE_CAPTURE_AGENT_ERRORS": "true",
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

	captured := e.capturedErrorsFor(t, jobID)
	if len(captured) != 1 || captured[0].Code != "buildkite.job_rejected" || !strings.HasPrefix(captured[0].Message, "This agent's pre-bootstrap hook rejected the job.") {
		t.Errorf("captured errors = %+v, want buildkite.job_rejected by the pre-bootstrap hook", captured)
	}
}

func TestJobRunner_WhenBootstrapExits_ItSendsTheExitStatusToTheAPI(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

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

func TestJobRunner_WhenBootstrapIsSignalled_ItSendsTheSignalledExitStatusToTheAPI(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	if runtime.GOOS == "windows" {
		t.Skip("signals don't exist on Windows")
	}

	j := &api.Job{
		ID:                 "my-job-id",
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
		},
		Token: "bkaj_job-token",
	}

	// A bootstrap that is killed by a signal has no exit status of its own
	// (ExitStatus is -1). The agent should report the shell-style 128+N
	// status and the signal, not -1 (which otherwise means "the command
	// never ran") or 255.
	bootstrapScript := filepath.Join(t.TempDir(), "bootstrap.sh")
	if err := os.WriteFile(bootstrapScript, []byte("#!/bin/sh\nkill -TERM $$\n"), 0o777); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", bootstrapScript, err)
	}

	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	err := runJob(t, ctx, testRunJobConfig{
		job:      j,
		server:   server,
		agentCfg: agent.AgentConfiguration{BootstrapScript: fmt.Sprintf("sh %q", bootstrapScript)},
	})
	if err != nil {
		t.Fatalf("runJob() error = %v", err)
	}

	finish := e.finishesFor(t, "my-job-id")[0]

	if got, want := finish.ExitStatus, "143"; got != want {
		t.Errorf("finish.ExitStatus = %q, want %q", got, want)
	}
	if got, want := finish.Signal, "SIGTERM"; got != want {
		t.Errorf("finish.Signal = %q, want %q", got, want)
	}
}

func TestJobRunner_WhenJobHasToken_ItOverridesAccessToken(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

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

func TestJobRunnerPassesCheckoutOverrideModeToBootstrapAndEnvFile(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	j := &api.Job{
		ID:                 "my-job-id",
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
		},
		Token: "bkaj_job-token",
	}

	mb := mockBootstrap(t)
	t.Cleanup(func() {
		if err := mb.CheckAndClose(t); err != nil {
			t.Errorf("mb.CheckAndClose(t) error = %v", err)
		}
	})

	mb.Expect().Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_CHECKOUT_OVERRIDE_MODE"), "strict"; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_CHECKOUT_OVERRIDE_MODE) = %q, want %q", got, want)
			c.Exit(1)
			return
		}

		envFile := c.GetEnv("BUILDKITE_ENV_FILE")
		contents, err := os.ReadFile(envFile)
		if err != nil {
			t.Errorf("os.ReadFile(%q) error = %v", envFile, err)
			c.Exit(1)
			return
		}

		if !strings.Contains(string(contents), "BUILDKITE_CHECKOUT_OVERRIDE_MODE") {
			t.Errorf("env file %q did not contain BUILDKITE_CHECKOUT_OVERRIDE_MODE", envFile)
			c.Exit(1)
			return
		}

		c.Exit(0)
	})

	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	err := runJob(t, ctx, testRunJobConfig{
		job:           j,
		server:        server,
		agentCfg:      agent.AgentConfiguration{CheckoutOverrideMode: env.CheckoutOverrideStrict},
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
	ctx := t.Context()

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
	ctx := t.Context()

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
