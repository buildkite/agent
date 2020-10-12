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

func setupHooksPath() (string, func(), error) {
	hooksPath, err := ioutil.TempDir("", "")
	if err != nil {
		return "", nil, err
	}
	return hooksPath, func() { os.RemoveAll(hooksPath) }, nil
}

// Since the hook doesn't really return anything, we can't really test stuff but at
// least make sure the code doesn't explode.
func TestShutdownHook(t *testing.T) {

	t.Run("with shutdown hook", func(t *testing.T) {
		hooksPath, closer, err := setupHooksPath()
		if err != nil {
			assert.FailNow(t, "failed to create temp file: %v", err)
		}
		defer closer()

		filename := "shutdown"
		if runtime.GOOS == "windows" {
			filename = "shutdown.bat"
		}
		err = ioutil.WriteFile(filepath.Join(hooksPath, filename), []byte("echo hello"), 0755)
		if err != nil {
			assert.FailNow(t, "failed to write shutdown hook: %v", err)
		}

		log := logger.NewBuffer()
		shutdownHook(log, hooksPath)
		assert.Equal(t, []string{}, log.Messages)
	})

	t.Run("with no shutdown hook", func(t *testing.T) {
		hooksPath, closer, err := setupHooksPath()
		if err != nil {
			assert.FailNow(t, "failed to create temp file: %v", err)
		}
		defer closer()

		log := logger.NewBuffer()
		shutdownHook(log, hooksPath)
		assert.Equal(t, []string{}, log.Messages)
	})

	t.Run("with bad hooks path", func(t *testing.T) {
		log := logger.NewBuffer()
		shutdownHook(log, "zxczxczxc")
		assert.Equal(t, []string{}, log.Messages)
	})
}
