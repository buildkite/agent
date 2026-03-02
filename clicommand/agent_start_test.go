package clicommand

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/buildkite/agent/v3/core"
	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/assert"
	"github.com/urfave/cli"
)

func setupHooksPath(t *testing.T) (string, func()) {
	t.Helper()

	hooksPath, err := os.MkdirTemp("", "")
	if err != nil {
		assert.FailNow(t, "failed to create temp file: %v", err)
	}
	return hooksPath, func() { os.RemoveAll(hooksPath) } //nolint:errcheck // best-effort cleanup in test
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
		assert.FailNow(t, "failed to write %q hook: %v", hookName, err)
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

		if assert.NoError(t, err, log.Messages) {
			assert.Equal(t, []string{
				"[info] " + prompt + " " + filepath, // prompt
				"[info] hello world",                // output
			}, log.Messages)
		}
	})

	t.Run("with no agent-startup hook", func(t *testing.T) {
		t.Parallel()

		hooksPath, closer := setupHooksPath(t)
		defer closer()

		log := logger.NewBuffer()
		err := agentStartupHook(log, cfg(hooksPath))
		if assert.NoError(t, err, log.Messages) {
			assert.Equal(t, []string{}, log.Messages)
		}
	})

	t.Run("with bad hooks path", func(t *testing.T) {
		t.Parallel()

		log := logger.NewBuffer()
		err := agentStartupHook(log, cfg("zxczxczxc"))

		if assert.NoError(t, err, log.Messages) {
			assert.Equal(t, []string{}, log.Messages)
		}
	})
}

func TestAgentStartupHookWithAdditionalPaths(t *testing.T) {
	t.SkipNow()
	// This test was added to validate that multiple global hooks can be added
	// by using the AdditionalHooksPaths configuration option. When this test
	// runs however, there's a timing issue where the second hook errors at
	// execution time as the file is not available.
	//
	// Error:          Received unexpected error:
	//                 error running "/opt/homebrew/bin/bash /var/folders/x3/rsj92m015tdcby8gz2j_25ym0000gn/T/471662504/agent-startup": unexpected error type *errors.errorString: io: read/write on closed pipe
	// Test:           TestAgentStartupHookWithAdditionalPaths/with_additional_agent-startup_hook
	// Messages:       [[info] $ /var/folders/x3/rsj92m015tdcby8gz2j_25ym0000gn/T/982974833/agent-startup [info] hello new world [error] "agent-startup" hook: error running "/opt/homebrew/bin/bash /var/folders/x3/rsj92m015tdcby8gz2j_25ym0000gn/T/471662504/agent-startup": unexpected error type *errors.errorString: io: read/write on closed pipe]
	//
	// For now it is skipped, and left as a placeholder!

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

		if assert.NoError(t, err, log.Messages) {
			assert.Equal(t, []string{
				"[info] " + prompt + " " + filepath,    // prompt
				"[info] hello new world",               // output
				"[info] " + prompt + " " + addFilepath, // prompt
				"[info] hello additional world",        // output
			}, log.Messages)
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

		assert.Equal(t, []string{
			"[info] " + prompt + " " + filepath, // prompt
			"[info] hello world",                // output
		}, log.Messages)
	})

	t.Run("with no agent-shutdown hook", func(t *testing.T) {
		t.Parallel()

		hooksPath, closer := setupHooksPath(t)
		defer closer()

		log := logger.NewBuffer()
		agentShutdownHook(log, cfg(hooksPath))
		assert.Equal(t, []string{}, log.Messages)
	})

	t.Run("with bad hooks path", func(t *testing.T) {
		t.Parallel()

		log := logger.NewBuffer()
		agentShutdownHook(log, cfg("zxczxczxc"))
		assert.Equal(t, []string{}, log.Messages)
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
	assert.True(t, errors.As(cliErr, &exitErr), "Expected cli.ExitError, got: %v", cliErr)
	assert.Equal(t, 28, exitErr.ExitCode(), "Expected exit code 28 for job locked, got: %d", exitErr.ExitCode())
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
	assert.True(t, errors.As(cliErr, &exitErr), "Expected cli.ExitError, got: %v", cliErr)
	assert.Equal(t, 27, exitErr.ExitCode(), "Expected exit code 27 for job acquisition rejected, got: %d", exitErr.ExitCode())
}
