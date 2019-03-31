// +build windows

package utils

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/assert"
	is "gotest.tools/assert/cmp"
)

func TestNormalizeWindowsDriveAbsolutePath(t *testing.T) {
	t.Parallel()

	fp, err := NormalizeFilePath(`C:\programdata\buildkite-agent`)

	assert.Check(t, err)
	assert.Check(t, is.Equal(`C:\programdata\buildkite-agent`, fp))
}

func TestNormalizeWindowsBackslashAbsolutePath(t *testing.T) {
	t.Parallel()

	// A naked backslash on Windows resolves to root of current working directory's drive.
	dir, err := os.Getwd()
	assert.NoError(t, err)
	drive := filepath.VolumeName(dir)
	fp, err := NormalizeFilePath(`\`)

	assert.Check(t, err)
	assert.Check(t, is.Equal(drive+`\`, fp))
}
