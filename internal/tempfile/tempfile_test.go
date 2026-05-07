package tempfile_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/tempfile"
)

func TestNew(t *testing.T) {
	t.Parallel()

	f, err := tempfile.New()
	if err != nil {
		t.Fatalf(`New() = %v`, err)
	}

	t.Cleanup(func() {
		if got := f.Close() == nil; !got {
			t.Errorf("failed to close file: %s", f.Name())
		}
		if got := os.Remove(f.Name()) == nil; !got {
			t.Errorf("failed to remove file: %s", f.Name())
		}
	})

	if got := strings.HasPrefix(f.Name(), os.TempDir()); !got {
		t.Fatalf("strings.HasPrefix(f.Name(), os.TempDir()) = %t, want true", got)
	}
}

func TestNewWithFilename(t *testing.T) {
	t.Parallel()

	f, err := tempfile.New(tempfile.WithName("foo.txt"))
	if err != nil {
		t.Fatalf(`New(WithName("foo.txt")) = %v`, err)
	}

	t.Cleanup(func() {
		if got := f.Close() == nil; !got {
			t.Errorf("failed to close file: %s", f.Name())
		}
		if got := os.Remove(f.Name()) == nil; !got {
			t.Errorf("failed to remove file: %s", f.Name())
		}
	})

	if got := strings.HasPrefix(f.Name(), os.TempDir()); !got {
		t.Fatalf("strings.HasPrefix(f.Name(), os.TempDir()) = %t, want true", got)
	}
}

func TestNewWithDir(t *testing.T) {
	t.Parallel()

	f, err := tempfile.New(tempfile.WithDir("TestNewWithDir"))
	if err != nil {
		t.Fatalf(`New(WithDir("TestNewWithDir")) = %v`, err)
	}

	dir := filepath.Join(os.TempDir(), "TestNewWithDir")

	t.Cleanup(func() {
		if got := f.Close() == nil; !got {
			t.Errorf("failed to close file: %s", f.Name())
		}
		if got := os.RemoveAll(dir) == nil; !got {
			t.Errorf("failed to remove dir: %s", dir)
		}
	})

	if got := strings.HasPrefix(f.Name(), dir); !got {
		t.Fatalf("strings.HasPrefix(f.Name(), dir) = %t, want true", got)
	}
}

func TestNewWithPerms(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows doesn't support or need checking if chmod worked")
	}

	f, err := tempfile.New(tempfile.WithPerms(0o600))
	if err != nil {
		t.Fatalf(`New(WithPerms(0o600)) = %v`, err)
	}

	t.Cleanup(func() {
		if got := f.Close() == nil; !got {
			t.Errorf("failed to close file: %s", f.Name())
		}
		if got := os.Remove(f.Name()) == nil; !got {
			t.Errorf("failed to remove file: %s", f.Name())
		}
	})

	fi, err := os.Stat(f.Name())
	if err != nil {
		t.Fatalf("os.Stat(%q) = %s", f.Name(), err)
	}

	if got := fi.Mode().Perm() == os.FileMode(0o600); !got {
		t.Fatalf("fi.Mode().Perm() == os.FileMode(0o600) = %t, want true", got)
	}
}

func TestNewWithFilenameAndKeepExtension(t *testing.T) {
	t.Parallel()

	f, err := tempfile.New(tempfile.WithName("foo.txt"), tempfile.KeepingExtension())
	if err != nil {
		t.Fatalf(`New(WithName("foo.txt"), KeepingExtension()) = %v`, err)
	}

	t.Cleanup(func() {
		if got := f.Close() == nil; !got {
			t.Errorf("failed to close file: %s", f.Name())
		}
		if got := os.Remove(f.Name()) == nil; !got {
			t.Errorf("failed to remove file: %s", f.Name())
		}
	})

	if got := filepath.Ext(f.Name()) == ".txt"; !got {
		t.Fatalf("filepath.Ext(f.Name()) == \".txt\" = %t, want true", got)
	}
}

func TestNewWithoutFilenameAndKeepExtension(t *testing.T) {
	t.Parallel()

	f, err := tempfile.New(tempfile.KeepingExtension())
	if err != nil {
		t.Fatalf(`New(KeepingExtension()) = %v`, err)
	}

	if got := f.Close() == nil; !got {
		t.Errorf("failed to close file: %s", f.Name())
	}
	if err := os.Remove(f.Name()); err != nil {
		t.Fatalf("failed to remove file: %s", f.Name())
	}
}

func TestNewClosed(t *testing.T) {
	t.Parallel()

	filename, err := tempfile.NewClosed()
	if err != nil {
		t.Fatalf(`NewClosed() = %v`, err)
	}

	t.Cleanup(func() {
		if got := os.Remove(filename) == nil; !got {
			t.Errorf("failed to remove file: %s", filename)
		}
	})

	if got := strings.HasPrefix(filename, os.TempDir()); !got {
		t.Fatalf("strings.HasPrefix(filename, os.TempDir()) = %t, want true", got)
	}
}

func TestNewClosedWithFilename(t *testing.T) {
	t.Parallel()

	filename, err := tempfile.NewClosed(tempfile.WithName("foo.txt"))
	if err != nil {
		t.Fatalf(`NewClosed(WithName("foo.txt")) = %v`, err)
	}

	t.Cleanup(func() {
		if got := os.Remove(filename) == nil; !got {
			t.Errorf("failed to remove file: %s", filename)
		}
	})

	if got := strings.HasPrefix(filename, os.TempDir()); !got {
		t.Fatalf("strings.HasPrefix(filename, os.TempDir()) = %t, want true", got)
	}
}

func TestNewClosedWithDir(t *testing.T) {
	t.Parallel()

	filename, err := tempfile.NewClosed(tempfile.WithDir("TestNewClosedWithDir"))
	if err != nil {
		t.Fatalf(`NewClosed(WithDir("TestNewClosedWithDir")) = %v`, err)
	}

	dir := filepath.Join(os.TempDir(), "TestNewClosedWithDir")

	t.Cleanup(func() {
		if got := os.RemoveAll(dir) == nil; !got {
			t.Errorf("failed to remove dir: %s", dir)
		}
	})

	if got := strings.HasPrefix(filename, dir); !got {
		t.Fatalf("strings.HasPrefix(filename, dir) = %t, want true", got)
	}
}

func TestNewClosedWithPerms(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows doesn't support or need checking if chmod worked")
	}

	filename, err := tempfile.NewClosed(tempfile.WithPerms(0o600))
	if err != nil {
		t.Fatalf(`NewClosed(WithPerms(0o600)) = %v`, err)
	}

	t.Cleanup(func() {
		if got := os.Remove(filename) == nil; !got {
			t.Errorf("failed to remove file: %s", filename)
		}
	})

	fi, err := os.Stat(filename)
	if err != nil {
		t.Fatalf("os.Stat(%q) = %s", filename, err)
	}

	if got := fi.Mode().Perm() == os.FileMode(0o600); !got {
		t.Fatalf("fi.Mode().Perm() == os.FileMode(0o600) = %t, want true", got)
	}
}

func TestNewClosedWithFilenameAndKeepExtension(t *testing.T) {
	t.Parallel()

	filename, err := tempfile.NewClosed(tempfile.WithName("foo.txt"), tempfile.KeepingExtension())
	if err != nil {
		t.Fatalf(`NewClosed(WithName("foo.txt"), KeepingExtension()) = %v`, err)
	}

	t.Cleanup(func() {
		if got := os.Remove(filename) == nil; !got {
			t.Errorf("failed to remove file: %s", filename)
		}
	})

	if got := filepath.Ext(filename) == ".txt"; !got {
		t.Fatalf("filepath.Ext(filename) == \".txt\" = %t, want true", got)
	}
}

func TestNewClosedWithoutFilenameAndKeepExtension(t *testing.T) {
	t.Parallel()

	filename, err := tempfile.NewClosed(tempfile.KeepingExtension())
	if err != nil {
		t.Fatalf(`NewClosed(KeepingExtension()) = %v`, err)
	}

	if err := os.Remove(filename); err != nil {
		t.Fatalf("failed to remove file: %s", filename)
	}
}
