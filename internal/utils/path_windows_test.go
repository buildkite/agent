//go:build windows
// +build windows

package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeWindowsDriveAbsolutePath(t *testing.T) {
	t.Parallel()

	fp, err := NormalizeFilePath(`C:\programdata\buildkite-agent`)
	assert.NoError(t, err)
	assert.Equal(t, `C:\programdata\buildkite-agent`, fp)
}

func TestNormalizeWindowsBackslashAbsolutePath(t *testing.T) {
	t.Parallel()

	// A naked backslash on Windows resolves to root of current working directory's drive.
	dir, err := os.Getwd()
	assert.NoError(t, err)
	drive := filepath.VolumeName(dir)
	fp, err := NormalizeFilePath(`\`)

	assert.NoError(t, err)
	assert.Equal(t, drive+`\`, fp)
}
