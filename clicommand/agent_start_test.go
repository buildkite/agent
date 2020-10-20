package clicommand

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/assert"
)

func setupHooksPath(t *testing.T) (string, func()) {
	hooksPath, err := ioutil.TempDir("", "")
	if err != nil {
		assert.FailNow(t, "failed to create temp file: %v", err)
	}
	return hooksPath, func() { os.RemoveAll(hooksPath) }
}

func writeAgentShutdownHook(t *testing.T, dir string) string {
	var filename, script string
	if runtime.GOOS == "windows" {
		filename = "agent-shutdown.bat"
		script = "@echo off\necho hello world"
	} else {
		filename = "agent-shutdown"
		script = "echo hello world"
	}
	filepath := filepath.Join(dir, filename)
	if err := ioutil.WriteFile(filepath, []byte(script), 0755); err != nil {
		assert.FailNow(t, "failed to write agent-shutdown hook: %v", err)
	}
	return filepath
}

func TestAgentShutdownHook(t *testing.T) {
	cfg := func(hooksPath string) AgentStartConfig {
		return AgentStartConfig{
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
		filepath := writeAgentShutdownHook(t, hooksPath)
		log := logger.NewBuffer()
		agentShutdownHook(log, cfg(hooksPath))

		assert.Equal(t, []string{
			"[info] " + prompt + " " + filepath, // prompt
			"[info] hello world",                // output
		}, log.Messages)
	})
	t.Run("with no agent-shutdown hook", func(t *testing.T) {
		hooksPath, closer := setupHooksPath(t)
		defer closer()

		log := logger.NewBuffer()
		agentShutdownHook(log, cfg(hooksPath))
		assert.Equal(t, []string{}, log.Messages)
	})
	t.Run("with bad hooks path", func(t *testing.T) {
		log := logger.NewBuffer()
		agentShutdownHook(log, cfg("zxczxczxc"))
		assert.Equal(t, []string{}, log.Messages)
	})
}
