package utils

import (
	"os"
	"os/user"
	"testing"
	"path/filepath"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeFilePath(t *testing.T) {
	t.Parallel()

	// `NormalizeFilePath` uses the current users home directory and the
	// current working dir, so we'll grab them now so we can make the right
	// assertions.
	usr, err := user.Current()
	assert.NoError(t, err)
	workingDir, err := os.Getwd()
	assert.NoError(t, err)

	fp, err := NormalizeFilePath(`/home/vagrant/repo`)
	assert.NoError(t, err)
	assert.Equal(t, `/home/vagrant/repo`, fp)
	assert.True(t, filepath.IsAbs(fp))

	fp, err = NormalizeFilePath(`~/.ssh`)
	assert.NoError(t, err)
	assert.Equal(t, usr.HomeDir + `/.ssh`, fp)
	assert.True(t, filepath.IsAbs(fp))

	fp, err = NormalizeFilePath(`./builds`)
	assert.NoError(t, err)
	assert.Equal(t, workingDir + `/builds`, fp)
	assert.True(t, filepath.IsAbs(fp))
}
