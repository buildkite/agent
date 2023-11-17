package tmpfile_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/tmpfile"
	"gotest.tools/v3/assert"
)

func TestKeepExtension(t *testing.T) {
	t.Parallel()

	f, err := tmpfile.KeepExtension("foo.txt")
	assert.NilError(t, err, `KeepExtension("foo.txt") = %v`, err)
	defer func() {
		assert.NilError(t, f.Close(), "failed to close file: %s", f.Name())
		assert.NilError(t, os.Remove(f.Name()), "failed to remove file: %s", f.Name())
	}()

	assert.Check(t, strings.HasPrefix(f.Name(), os.TempDir()))
	assert.Check(t, filepath.Ext(f.Name()) == ".txt")
}

func TestKeepExtensionAndClose(t *testing.T) {
	t.Parallel()

	filename, err := tmpfile.KeepExtensionAndClose("buildkite-agent", "foo.txt")
	assert.NilError(t, err, `KeepExtension("foo.txt") = %v`, err)

	assert.Check(t, strings.HasPrefix(filename, os.TempDir()))
	assert.Check(t, filepath.Ext(filename) == ".txt")
}

func TestKeepExtensionWithMode(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows doesn't support or need checking if chmod worked")
	}

	f, err := tmpfile.KeepExtensionWithMode("buildkite-agent", "foo.txt", 0o644)
	assert.NilError(t, err, `KeepExtensionWithMode("buildkite-agent", "foo.txt", 0o644) = %v`, err)
	defer func() {
		assert.NilError(t, f.Close(), "failed to close file: %s", f.Name())
		assert.NilError(t, os.Remove(f.Name()), "failed to remove file: %s", f.Name())
	}()

	fi, err := os.Stat(f.Name())
	assert.NilError(t, err, "os.Stat(%q) = %s", f.Name(), err)

	assert.Check(t, strings.HasPrefix(f.Name(), filepath.Join(os.TempDir(), "buildkite-agent")))
	assert.Check(t, filepath.Ext(f.Name()) == ".txt")
	assert.Check(t, fi.Mode().Perm() == os.FileMode(0o644))
}

func TestKeepExtensionWithModeAndClose(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows doesn't support or need checking if chmod worked")
	}

	filename, err := tmpfile.KeepExtensionWithModeAndClose("buildkite-agent", "foo.txt", 0o644)
	assert.NilError(t, err, `KeepExtensionWithModeAndClose("buildkite-agent", "foo.txt", 0o644) = %v`, err)

	fi, err := os.Stat(filename)
	assert.NilError(t, err, "os.Stat(%q) = %s", filename, err)

	assert.Check(t, strings.HasPrefix(filename, filepath.Join(os.TempDir(), "buildkite-agent")))
	assert.Check(t, filepath.Ext(filename) == ".txt")
	assert.Check(t, fi.Mode().Perm() == os.FileMode(0o644))
}
