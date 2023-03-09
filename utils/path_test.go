package utils

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizingHomeDirectories(t *testing.T) {
	t.Parallel()

	usr, err := user.Current()
	assert.NoError(t, err)

	fp, err := NormalizeFilePath(filepath.Join("~", ".ssh"))
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(usr.HomeDir, ".ssh"), fp)
	assert.True(t, filepath.IsAbs(fp))
}

func TestNormalizingFilePaths(t *testing.T) {
	t.Parallel()

	workingDir, err := os.Getwd()
	assert.NoError(t, err)

	fp, err := NormalizeFilePath(filepath.Join(".", "builds"))
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(workingDir, "builds"), fp)
	assert.True(t, filepath.IsAbs(fp))
}

func TestNormalizingEmptyPaths(t *testing.T) {
	t.Parallel()

	fp, err := NormalizeFilePath("")
	assert.NoError(t, err)
	assert.Equal(t, "", fp)
}

func TestNormalizingCommands(t *testing.T) {
	t.Parallel()

	usr, err := user.Current()
	assert.NoError(t, err)

	c, err := NormalizeCommand(filepath.Join("~/", "buildkite-agent", "job", "run"))
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(usr.HomeDir, "buildkite-agent", "job", "run"), c)

	c, err = NormalizeCommand("cat test.log")
	assert.NoError(t, err)
	assert.Equal(t, c, "cat test.log")
}
