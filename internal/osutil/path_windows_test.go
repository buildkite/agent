//go:build windows

package osutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeWindowsDriveAbsolutePath(t *testing.T) {
	t.Parallel()

	fp, err := NormalizeFilePath(`C:\programdata\buildkite-agent`)
	if err != nil {
		t.Errorf("NormalizeFilePath(%q) error = %v, want nil", `C:\programdata\buildkite-agent`, err)
	}
	if got, want := fp, `C:\programdata\buildkite-agent`; got != want {
		t.Errorf("NormalizeFilePath(%q) = %q, want %q", `C:\programdata\buildkite-agent`, got, want)
	}
}

func TestNormalizeWindowsBackslashAbsolutePath(t *testing.T) {
	t.Parallel()

	// A naked backslash on Windows resolves to root of current working directory's drive.
	dir, err := os.Getwd()
	if err != nil {
		t.Errorf("os.Getwd() error = %v, want nil", err)
	}
	drive := filepath.VolumeName(dir)
	fp, err := NormalizeFilePath(`\`)
	if err != nil {
		t.Errorf("NormalizeFilePath(%q) error = %v, want nil", `\`, err)
	}
	if got, want := fp, drive+`\`; got != want {
		t.Errorf("NormalizeFilePath(%q) = %q, want %q", `\`, got, want)
	}
}
