package utils

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"gotest.tools/assert"
	is "gotest.tools/assert/cmp"
)

func TestNormalizingHomeDirectories(t *testing.T) {
	t.Parallel()

	usr, err := user.Current()
	assert.Check(t, err)

	fp, err := NormalizeFilePath(filepath.Join(`~`, `.ssh`))
	assert.Check(t, err)
	assert.Check(t, is.Equal(filepath.Join(usr.HomeDir, `.ssh`), fp))
	assert.Check(t, filepath.IsAbs(fp))
}

func TestNormalizingFilePaths(t *testing.T) {
	t.Parallel()

	workingDir, err := os.Getwd()
	assert.Check(t, err)

	fp, err := NormalizeFilePath(filepath.Join(`.`, `builds`))
	assert.Check(t, err)
	assert.Check(t, is.Equal(filepath.Join(workingDir, `builds`), fp))
	assert.Check(t, filepath.IsAbs(fp))
}

func TestNormalizingEmptyPaths(t *testing.T) {
	t.Parallel()

	fp, err := NormalizeFilePath("")
	assert.Check(t, err)
	assert.Check(t, is.Equal("", fp))
}

func TestNormalizingCommands(t *testing.T) {
	t.Parallel()

	usr, err := user.Current()
	assert.Check(t, err)

	c, err := NormalizeCommand(filepath.Join(`~/`, `buildkite-agent`, `bootstrap.sh`))
	assert.Check(t, err)
	assert.Check(t, is.Equal(filepath.Join(usr.HomeDir, `buildkite-agent`, `bootstrap.sh`), c))

	c, err = NormalizeCommand("cat test.log")
	assert.Check(t, err)
	assert.Check(t, is.Equal(c, "cat test.log"))
}
