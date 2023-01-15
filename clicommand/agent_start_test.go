package clicommand_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/buildkite/agent/v3/clicommand"
	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/assert"
)

func setupHooksPath(t *testing.T) (string, func()) {
	hooksPath, err := os.MkdirTemp("", "")
	if err != nil {
		assert.FailNow(t, "failed to create temp file: %v", err)
	}
	return hooksPath, func() { os.RemoveAll(hooksPath) }
}

func writeAgentHook(t *testing.T, dir, hookName string) string {
	var filename, script string
	if runtime.GOOS == "windows" {
		filename = hookName + ".bat"
		script = "@echo off\necho hello world"
	} else {
		filename = hookName
		script = "echo hello world"
	}
	filepath := filepath.Join(dir, filename)
	if err := os.WriteFile(filepath, []byte(script), 0755); err != nil {
		assert.FailNow(t, "failed to write %q hook: %v", hookName, err)
	}
	return filepath
}

func TestAgentStartupHook(t *testing.T) {
	cfg := func(hooksPath string) clicommand.AgentStartConfig {
		return clicommand.AgentStartConfig{
			HooksPath: hooksPath,
			NoColor:   true,
		}
	}
	prompt := "$"
	if runtime.GOOS == "windows" {
		prompt = ">"
	}
	t.Run("with agent-startup hook", func(t *testing.T) {
		hooksPath, closer := setupHooksPath(t)
		defer closer()
		filepath := writeAgentHook(t, hooksPath, "agent-startup")
		log := logger.NewBuffer()
		err := clicommand.AgentStartupHook(log, cfg(hooksPath))
		if assert.NoError(t, err, log.Messages) {
			assert.Equal(t, []string{
				"[info] " + prompt + " " + filepath, // prompt
				"[info] hello world",                // output
			}, log.Messages)
		}
	})
	t.Run("with no agent-startup hook", func(t *testing.T) {
		hooksPath, closer := setupHooksPath(t)
		defer closer()

		log := logger.NewBuffer()
		err := clicommand.AgentStartupHook(log, cfg(hooksPath))
		if assert.NoError(t, err, log.Messages) {
			assert.Equal(t, []string{}, log.Messages)
		}
	})
	t.Run("with bad hooks path", func(t *testing.T) {
		log := logger.NewBuffer()
		err := clicommand.AgentStartupHook(log, cfg("zxczxczxc"))
		if assert.NoError(t, err, log.Messages) {
			assert.Equal(t, []string{}, log.Messages)
		}
	})
}

func TestAgentShutdownHook(t *testing.T) {
	cfg := func(hooksPath string) clicommand.AgentStartConfig {
		return clicommand.AgentStartConfig{
			HooksPath: hooksPath,
			NoColor:   true,
		}
	}
	prompt := "$"
	if runtime.GOOS == "windows" {
		prompt = ">"
	}
	t.Run("with agent-shutdown hook", func(t *testing.T) {
		hooksPath, closer := setupHooksPath(t)
		defer closer()
		filepath := writeAgentHook(t, hooksPath, "agent-shutdown")
		log := logger.NewBuffer()
		clicommand.AgentShutdownHook(log, cfg(hooksPath))

		assert.Equal(t, []string{
			"[info] " + prompt + " " + filepath, // prompt
			"[info] hello world",                // output
		}, log.Messages)
	})
	t.Run("with no agent-shutdown hook", func(t *testing.T) {
		hooksPath, closer := setupHooksPath(t)
		defer closer()

		log := logger.NewBuffer()
		clicommand.AgentShutdownHook(log, cfg(hooksPath))
		assert.Equal(t, []string{}, log.Messages)
	})
	t.Run("with bad hooks path", func(t *testing.T) {
		log := logger.NewBuffer()
		clicommand.AgentShutdownHook(log, cfg("zxczxczxc"))
		assert.Equal(t, []string{}, log.Messages)
	})
}
