package clicommand

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveWorkdir is deliberately not parallel: the "relative path" subtest
// changes the process working directory, which would corrupt other tests if run
// concurrently. Non-parallel tests run during testing's sequential phase, when
// no parallel test is active.
func TestResolveWorkdir(t *testing.T) {
	dir := t.TempDir()

	t.Run("absolute existing directory", func(t *testing.T) {
		got, err := resolveWorkdir(dir)
		if err != nil {
			t.Fatalf("resolveWorkdir(%q) error = %v, want nil", dir, err)
		}
		if got != dir {
			t.Fatalf("resolveWorkdir(%q) = %q, want %q", dir, got, dir)
		}
	})

	t.Run("relative path is resolved against cwd", func(t *testing.T) {
		// Not parallel: changes the process working directory.
		sub := "workdir-sub"
		if err := os.Mkdir(filepath.Join(dir, sub), 0o700); err != nil {
			t.Fatalf("os.Mkdir error = %v", err)
		}

		prev, err := os.Getwd()
		if err != nil {
			t.Fatalf("os.Getwd() error = %v", err)
		}
		t.Cleanup(func() { _ = os.Chdir(prev) })
		if err := os.Chdir(dir); err != nil {
			t.Fatalf("os.Chdir(%q) error = %v", dir, err)
		}

		// Compute want from the actual cwd, since os.Getwd (used by filepath.Abs)
		// may resolve symlinks (e.g. /tmp -> /private/tmp on macOS).
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("os.Getwd() error = %v", err)
		}

		got, err := resolveWorkdir(sub)
		if err != nil {
			t.Fatalf("resolveWorkdir(%q) error = %v, want nil", sub, err)
		}
		want := filepath.Join(cwd, sub)
		if got != want {
			t.Fatalf("resolveWorkdir(%q) = %q, want %q", sub, got, want)
		}
	})

	t.Run("nonexistent path errors", func(t *testing.T) {
		missing := filepath.Join(dir, "does-not-exist")
		if _, err := resolveWorkdir(missing); err == nil {
			t.Fatalf("resolveWorkdir(%q) error = nil, want non-nil", missing)
		}
	})

	t.Run("file (not a directory) errors", func(t *testing.T) {
		file := filepath.Join(dir, "a-file")
		if err := os.WriteFile(file, []byte("hi"), 0o600); err != nil {
			t.Fatalf("os.WriteFile error = %v", err)
		}
		if _, err := resolveWorkdir(file); err == nil {
			t.Fatalf("resolveWorkdir(%q) error = nil, want non-nil", file)
		}
	})
}
