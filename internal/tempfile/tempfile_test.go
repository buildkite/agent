package tempfile_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/tempfile"
	"gotest.tools/v3/assert"
)

func TestNew(t *testing.T) {
	t.Parallel()

	f, err := tempfile.New()
	assert.NilError(t, err, `New() = %v`, err)

	t.Cleanup(func() {
		assert.Check(t, f.Close() == nil, "failed to close file: %s", f.Name())
		assert.Check(t, os.Remove(f.Name()) == nil, "failed to remove file: %s", f.Name())
	})

	assert.Assert(t, strings.HasPrefix(f.Name(), os.TempDir()))
}

func TestNewWithFilename(t *testing.T) {
	t.Parallel()

	f, err := tempfile.New(tempfile.WithName("foo.txt"))
	assert.NilError(t, err, `New(WithName("foo.txt")) = %v`, err)

	t.Cleanup(func() {
		assert.Check(t, f.Close() == nil, "failed to close file: %s", f.Name())
		assert.Check(t, os.Remove(f.Name()) == nil, "failed to remove file: %s", f.Name())
	})

	assert.Assert(t, strings.HasPrefix(f.Name(), os.TempDir()))
}

func TestNewWithDir(t *testing.T) {
	t.Parallel()

	f, err := tempfile.New(tempfile.WithDir("TestNewWithDir"))
	assert.NilError(t, err, `New(WithDir("TestNewWithDir")) = %v`, err)

	dir := filepath.Join(os.TempDir(), "TestNewWithDir")

	t.Cleanup(func() {
		assert.Check(t, f.Close() == nil, "failed to close file: %s", f.Name())
		assert.Check(t, os.RemoveAll(dir) == nil, "failed to remove dir: %s", dir)
	})

	assert.Assert(t, strings.HasPrefix(f.Name(), dir))
}

func TestNewWithPerms(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows doesn't support or need checking if chmod worked")
	}

	f, err := tempfile.New(tempfile.WithPerms(0o600))
	assert.NilError(t, err, `New(WithPerms(0o600)) = %v`, err)

	t.Cleanup(func() {
		assert.Check(t, f.Close() == nil, "failed to close file: %s", f.Name())
		assert.Check(t, os.Remove(f.Name()) == nil, "failed to remove file: %s", f.Name())
	})

	fi, err := os.Stat(f.Name())
	assert.NilError(t, err, "os.Stat(%q) = %s", f.Name(), err)

	assert.Assert(t, fi.Mode().Perm() == os.FileMode(0o600))
}

func TestNewWithFilenameAndKeepExtension(t *testing.T) {
	t.Parallel()

	f, err := tempfile.New(tempfile.WithName("foo.txt"), tempfile.KeepingExtension())
	assert.NilError(t, err, `New(WithName("foo.txt"), KeepingExtension()) = %v`, err)

	t.Cleanup(func() {
		assert.Check(t, f.Close() == nil, "failed to close file: %s", f.Name())
		assert.Check(t, os.Remove(f.Name()) == nil, "failed to remove file: %s", f.Name())
	})

	assert.Assert(t, filepath.Ext(f.Name()) == ".txt")
}

func TestNewWithoutFilenameAndKeepExtension(t *testing.T) {
	t.Parallel()

	f, err := tempfile.New(tempfile.KeepingExtension())
	assert.NilError(t, err, `New(KeepingExtension()) = %v`, err)

	assert.Check(t, f.Close() == nil, "failed to close file: %s", f.Name())
	assert.NilError(t, os.Remove(f.Name()), "failed to remove file: %s", f.Name())
}

func TestNewClosed(t *testing.T) {
	t.Parallel()

	filename, err := tempfile.NewClosed()
	assert.NilError(t, err, `NewClosed() = %v`, err)

	t.Cleanup(func() {
		assert.Check(t, os.Remove(filename) == nil, "failed to remove file: %s", filename)
	})

	assert.Assert(t, strings.HasPrefix(filename, os.TempDir()))
}

func TestNewClosedWithFilename(t *testing.T) {
	t.Parallel()

	filename, err := tempfile.NewClosed(tempfile.WithName("foo.txt"))
	assert.NilError(t, err, `NewClosed(WithName("foo.txt")) = %v`, err)

	t.Cleanup(func() {
		assert.Check(t, os.Remove(filename) == nil, "failed to remove file: %s", filename)
	})

	assert.Assert(t, strings.HasPrefix(filename, os.TempDir()))
}

func TestNewClosedWithDir(t *testing.T) {
	t.Parallel()

	filename, err := tempfile.NewClosed(tempfile.WithDir("TestNewClosedWithDir"))
	assert.NilError(t, err, `NewClosed(WithDir("TestNewClosedWithDir")) = %v`, err)

	dir := filepath.Join(os.TempDir(), "TestNewClosedWithDir")

	t.Cleanup(func() {
		assert.Check(t, os.RemoveAll(dir) == nil, "failed to remove dir: %s", dir)
	})

	assert.Assert(t, strings.HasPrefix(filename, dir))
}

func TestNewClosedWithPerms(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows doesn't support or need checking if chmod worked")
	}

	filename, err := tempfile.NewClosed(tempfile.WithPerms(0o600))
	assert.NilError(t, err, `NewClosed(WithPerms(0o600)) = %v`, err)

	t.Cleanup(func() {
		assert.Check(t, os.Remove(filename) == nil, "failed to remove file: %s", filename)
	})

	fi, err := os.Stat(filename)
	assert.NilError(t, err, "os.Stat(%q) = %s", filename, err)

	assert.Assert(t, fi.Mode().Perm() == os.FileMode(0o600))
}

func TestNewClosedWithFilenameAndKeepExtension(t *testing.T) {
	t.Parallel()

	filename, err := tempfile.NewClosed(tempfile.WithName("foo.txt"), tempfile.KeepingExtension())
	assert.NilError(t, err, `NewClosed(WithName("foo.txt"), KeepingExtension()) = %v`, err)

	t.Cleanup(func() {
		assert.Check(t, os.Remove(filename) == nil, "failed to remove file: %s", filename)
	})

	assert.Assert(t, filepath.Ext(filename) == ".txt")
}

func TestNewClosedWithoutFilenameAndKeepExtension(t *testing.T) {
	t.Parallel()

	filename, err := tempfile.NewClosed(tempfile.KeepingExtension())
	assert.NilError(t, err, `NewClosed(KeepingExtension()) = %v`, err)

	assert.NilError(t, os.Remove(filename), "failed to remove file: %s", filename)
}
