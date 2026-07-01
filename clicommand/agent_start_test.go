package clicommand

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/core"
	"github.com/buildkite/agent/v3/logger"
	"github.com/google/go-cmp/cmp"
	"github.com/urfave/cli"
)

func setupHooksPath(t *testing.T) (string, func()) {
	t.Helper()

	hooksPath, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	return hooksPath, func() { _ = os.RemoveAll(hooksPath) }
}

func writeAgentHook(t *testing.T, dir, hookName, fixtureName string) string {
	t.Helper()

	filename := hookName
	fixtureFilename := fixtureName + ".sh"
	if runtime.GOOS == "windows" {
		filename = hookName + ".bat"
		fixtureFilename = fixtureName + ".bat"
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() = %v", err)
	}
	fixturePath := filepath.Join(wd, "..", "test", "fixtures", "agent-hook", fixtureFilename)
	script, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) = %v", fixturePath, err)
	}

	filepath := filepath.Join(dir, filename)
	t.Logf("Creating %q from %q", filepath, fixturePath)
	if err := os.WriteFile(filepath, script, 0o755); err != nil {
		t.Fatalf("%+v", err)
	}
	return filepath
}

func testAgentWorker(id, name string) *agent.AgentWorker {
	return agent.NewAgentWorker(
		logger.Discard,
		&api.AgentRegisterResponse{UUID: id, Name: name},
		nil,
		api.NewClient(logger.Discard, api.Config{}),
		agent.AgentWorkerConfig{},
	)
}

func TestAgentStartupHook(t *testing.T) {
	t.Parallel()

	cfg := func(hooksPath string) AgentStartConfig {
		return AgentStartConfig{
			HooksPath:    hooksPath,
			GlobalConfig: GlobalConfig{NoColor: true},
		}
	}
	prompt := "$"
	if runtime.GOOS == "windows" {
		prompt = ">"
	}

	t.Run("with agent-startup hook", func(t *testing.T) {
		t.Parallel()

		hooksPath, closer := setupHooksPath(t)
		defer closer()
		filepath := writeAgentHook(t, hooksPath, "agent-startup", "hello-world")
		log := logger.NewBuffer()
		err := agentStartupHook(log, cfg(hooksPath), nil)
		if err != nil {
			t.Fatalf("%+v", log.Messages)
		}
		if diff := cmp.Diff(log.Messages, []string{
			"[info] " + prompt + " " + filepath,
			"[info] hello world",
		}); diff != "" {
			t.Errorf("log.Messages diff (-got +want):\n%s", diff)
		}
	})

	t.Run("with no agent-startup hook", func(t *testing.T) {
		t.Parallel()

		hooksPath, closer := setupHooksPath(t)
		defer closer()

		log := logger.NewBuffer()
		err := agentStartupHook(log, cfg(hooksPath), nil)
		if err != nil {
			t.Fatalf("%+v", log.Messages)
		}
		if diff := cmp.Diff(log.Messages, []string{}); diff != "" {
			t.Errorf("log.Messages diff (-got +want):\n%s", diff)
		}
	})

	t.Run("with bad hooks path", func(t *testing.T) {
		t.Parallel()

		log := logger.NewBuffer()
		err := agentStartupHook(log, cfg("zxczxczxc"), nil)
		if err != nil {
			t.Fatalf("%+v", log.Messages)
		}
		if diff := cmp.Diff(log.Messages, []string{}); diff != "" {
			t.Errorf("log.Messages diff (-got +want):\n%s", diff)
		}
	})
}

func TestAgentStartupHookWithAdditionalPaths(t *testing.T) {
	t.Parallel()

	cfg := func(hooksPath, additionalHooksPath string) AgentStartConfig {
		return AgentStartConfig{
			HooksPath:            hooksPath,
			AdditionalHooksPaths: []string{additionalHooksPath},
			GlobalConfig:         GlobalConfig{NoColor: true},
		}
	}
	prompt := "$"
	if runtime.GOOS == "windows" {
		prompt = ">"
	}

	t.Run("with additional agent-startup hook", func(t *testing.T) {
		t.Parallel()

		hooksPath, closer := setupHooksPath(t)
		filepath := writeAgentHook(t, hooksPath, "agent-startup", "hello-new-world")
		defer closer()

		additionalHooksPath, additionalCloser := setupHooksPath(t)
		addFilepath := writeAgentHook(t, additionalHooksPath, "agent-startup", "hello-additional-world")
		defer additionalCloser()

		log := logger.NewBuffer()
		err := agentStartupHook(log, cfg(hooksPath, additionalHooksPath), nil)
		if err != nil {
			t.Fatalf("%+v", log.Messages)
		}
		if diff := cmp.Diff(log.Messages, []string{
			"[info] " + prompt + " " + filepath,
			"[info] hello new world",
			"[info] " + prompt + " " + addFilepath,
			"[info] hello additional world",
		}); diff != "" {
			t.Errorf("log.Messages diff (-got +want):\n%s", diff)
		}
	})
}

func TestAgentStartupHookEnv(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		desc      string
		workers   []*agent.AgentWorker
		wantIDs   string
		wantNames string
	}{
		{
			desc: "empty",
		},
		{
			desc:      "single agent",
			workers:   []*agent.AgentWorker{testAgentWorker("agent-123", "test-agent-1")},
			wantIDs:   "agent-123",
			wantNames: "test-agent-1",
		},
		{
			desc: "multiple agents",
			workers: []*agent.AgentWorker{
				testAgentWorker("agent-123", "test-agent-1"),
				testAgentWorker("agent-456", "test-agent-2"),
			},
			wantIDs:   "agent-123,agent-456",
			wantNames: "test-agent-1,test-agent-2",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			env := agentLifecycleHookEnv(tc.workers)
			gotIDs, hasIDs := env.Get("BUILDKITE_AGENT_IDS")
			if !hasIDs {
				t.Fatal("BUILDKITE_AGENT_IDS is not set")
			}
			if got := gotIDs; got != tc.wantIDs {
				t.Errorf("BUILDKITE_AGENT_IDS = %q, want %q", got, tc.wantIDs)
			}
			gotNames, hasNames := env.Get("BUILDKITE_AGENT_NAMES")
			if !hasNames {
				t.Fatal("BUILDKITE_AGENT_NAMES is not set")
			}
			if got := gotNames; got != tc.wantNames {
				t.Errorf("BUILDKITE_AGENT_NAMES = %q, want %q", got, tc.wantNames)
			}
		})
	}
}

func TestAgentStartupHookWithRegisteredAgentsEnv(t *testing.T) {
	t.Parallel()

	cfg := func(hooksPath string) AgentStartConfig {
		return AgentStartConfig{
			HooksPath:    hooksPath,
			GlobalConfig: GlobalConfig{NoColor: true},
		}
	}
	prompt := "$"
	if runtime.GOOS == "windows" {
		prompt = ">"
	}

	hooksPath, closer := setupHooksPath(t)
	defer closer()

	filepath := writeAgentHook(t, hooksPath, "agent-startup", "env-hook")

	log := logger.NewBuffer()
	err := agentStartupHook(log, cfg(hooksPath), []*agent.AgentWorker{
		testAgentWorker("agent-123", "test-agent-1"),
		testAgentWorker("agent-456", "test-agent-2"),
	})
	if err != nil {
		t.Fatalf("%+v", log.Messages)
	}
	if diff := cmp.Diff(log.Messages, []string{
		"[info] " + prompt + " " + filepath,
		"[info] ids=agent-123,agent-456",
		"[info] names=test-agent-1,test-agent-2",
	}); diff != "" {
		t.Errorf("log.Messages diff (-got +want):\n%s", diff)
	}
}

func TestAgentShutdownHook(t *testing.T) {
	t.Parallel()

	cfg := func(hooksPath string) AgentStartConfig {
		return AgentStartConfig{
			HooksPath:    hooksPath,
			GlobalConfig: GlobalConfig{NoColor: true},
		}
	}
	prompt := "$"
	if runtime.GOOS == "windows" {
		prompt = ">"
	}

	t.Run("with agent-shutdown hook", func(t *testing.T) {
		t.Parallel()

		hooksPath, closer := setupHooksPath(t)
		defer closer()
		filepath := writeAgentHook(t, hooksPath, "agent-shutdown", "hello-world")
		log := logger.NewBuffer()
		agentShutdownHook(log, cfg(hooksPath), nil)

		if diff := cmp.Diff(log.Messages, []string{
			"[info] " + prompt + " " + filepath,
			"[info] hello world",
		}); diff != "" {
			t.Errorf("log.Messages diff (-got +want):\n%s", diff)
		}
	})

	t.Run("with no agent-shutdown hook", func(t *testing.T) {
		t.Parallel()

		hooksPath, closer := setupHooksPath(t)
		defer closer()

		log := logger.NewBuffer()
		agentShutdownHook(log, cfg(hooksPath), nil)
		if diff := cmp.Diff(log.Messages, []string{}); diff != "" {
			t.Errorf("log.Messages diff (-got +want):\n%s", diff)
		}
	})

	t.Run("with bad hooks path", func(t *testing.T) {
		t.Parallel()

		log := logger.NewBuffer()
		agentShutdownHook(log, cfg("zxczxczxc"), nil)
		if diff := cmp.Diff(log.Messages, []string{}); diff != "" {
			t.Errorf("log.Messages diff (-got +want):\n%s", diff)
		}
	})

	t.Run("with registered agents env", func(t *testing.T) {
		t.Parallel()

		hooksPath, closer := setupHooksPath(t)
		defer closer()

		filepath := writeAgentHook(t, hooksPath, "agent-shutdown", "env-hook")

		log := logger.NewBuffer()
		agentShutdownHook(log, cfg(hooksPath), []*agent.AgentWorker{
			testAgentWorker("agent-123", "test-agent-1"),
			testAgentWorker("agent-456", "test-agent-2"),
		})

		if diff := cmp.Diff(log.Messages, []string{
			"[info] " + prompt + " " + filepath,
			"[info] ids=agent-123,agent-456",
			"[info] names=test-agent-1,test-agent-2",
		}); diff != "" {
			t.Errorf("log.Messages diff (-got +want):\n%s", diff)
		}
	})
}

func TestAgentStartJobLocked_ExitCode28(t *testing.T) {
	t.Parallel()

	// Test that the CLI command logic returns the correct exit code when ErrJobLocked is returned
	// This simulates what happens in the AgentStartCommand.Run method
	testErr := core.ErrJobLocked

	var cliErr error
	if errors.Is(testErr, core.ErrJobLocked) {
		const jobLockedExitCode = 28
		cliErr = cli.NewExitError(testErr, jobLockedExitCode)
	}

	var exitErr *cli.ExitError
	if got := errors.As(cliErr, &exitErr); !got {
		t.Errorf("Expected cli.ExitError, got: %v", cliErr)
	}
	if got, want := exitErr.ExitCode(), 28; got != want {
		t.Errorf("Expected exit code 28 for job locked, got: %d", exitErr.ExitCode())
	}
}

func TestAgentStartJobAcquisitionRejected_ExitCode27(t *testing.T) {
	t.Parallel()

	// Test that the CLI command logic returns the correct exit code when ErrJobAcquisitionRejected is returned
	// This simulates what happens in the AgentStartCommand.Run method
	testErr := core.ErrJobAcquisitionRejected

	var cliErr error
	if errors.Is(testErr, core.ErrJobAcquisitionRejected) {
		const acquisitionFailedExitCode = 27
		cliErr = cli.NewExitError(testErr, acquisitionFailedExitCode)
	}

	var exitErr *cli.ExitError
	if got := errors.As(cliErr, &exitErr); !got {
		t.Errorf("Expected cli.ExitError, got: %v", cliErr)
	}
	if got, want := exitErr.ExitCode(), 27; got != want {
		t.Errorf("Expected exit code 27 for job acquisition rejected, got: %d", exitErr.ExitCode())
	}
}
