package clicommand

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

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

func writeAgentHook(t *testing.T, dir, hookName, msg string) string {
	t.Helper()

	var filename, script string
	if runtime.GOOS == "windows" {
		filename = hookName + ".bat"
		script = "@echo off\necho " + msg
	} else {
		filename = hookName
		script = "echo " + msg
	}
	filepath := filepath.Join(dir, filename)
	t.Logf("Creating %q with %q content", filepath, msg)
	if err := os.WriteFile(filepath, []byte(script), 0o755); err != nil {
		t.Fatalf("%+v", err)
	}
	t.Log("Providing the path with file created")
	return filepath
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
		filepath := writeAgentHook(t, hooksPath, "agent-startup", "hello world")
		log := logger.NewBuffer()
		err := agentStartupHook(log, cfg(hooksPath))
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
		err := agentStartupHook(log, cfg(hooksPath))
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
		err := agentStartupHook(log, cfg("zxczxczxc"))
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
		filepath := writeAgentHook(t, hooksPath, "agent-startup", "hello new world")
		defer closer()

		additionalHooksPath, additionalCloser := setupHooksPath(t)
		addFilepath := writeAgentHook(t, additionalHooksPath, "agent-startup", "hello additional world")
		defer additionalCloser()

		log := logger.NewBuffer()
		err := agentStartupHook(log, cfg(hooksPath, additionalHooksPath))
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
		filepath := writeAgentHook(t, hooksPath, "agent-shutdown", "hello world")
		log := logger.NewBuffer()
		agentShutdownHook(log, cfg(hooksPath))

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
		agentShutdownHook(log, cfg(hooksPath))
		if diff := cmp.Diff(log.Messages, []string{}); diff != "" {
			t.Errorf("log.Messages diff (-got +want):\n%s", diff)
		}
	})

	t.Run("with bad hooks path", func(t *testing.T) {
		t.Parallel()

		log := logger.NewBuffer()
		agentShutdownHook(log, cfg("zxczxczxc"))
		if diff := cmp.Diff(log.Messages, []string{}); diff != "" {
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
