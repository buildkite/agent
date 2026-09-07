package clicommand

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveCacheConfigFile(t *testing.T) {
	t.Run("explicit path is used as-is", func(t *testing.T) {
		got, err := resolveCacheConfigFile("custom/cache.yml")
		if err != nil {
			t.Fatalf("resolveCacheConfigFile() error = %v", err)
		}
		if want := "custom/cache.yml"; got != want {
			t.Errorf("resolveCacheConfigFile() = %q, want %q", got, want)
		}
	})

	t.Run("explicit path skips default search", func(t *testing.T) {
		dir := t.TempDir()
		// Create both defaults; an explicit path must still win.
		writeFile(t, filepath.Join(dir, ".buildkite", "cache.yml"))
		writeFile(t, filepath.Join(dir, ".buildkite", "cache.yaml"))
		chdir(t, dir)

		got, err := resolveCacheConfigFile("custom/cache.yml")
		if err != nil {
			t.Fatalf("resolveCacheConfigFile() error = %v", err)
		}
		if want := "custom/cache.yml"; got != want {
			t.Errorf("resolveCacheConfigFile() = %q, want %q", got, want)
		}
	})

	t.Run("finds .yml when only .yml exists", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".buildkite", "cache.yml"))
		chdir(t, dir)

		got, err := resolveCacheConfigFile("")
		if err != nil {
			t.Fatalf("resolveCacheConfigFile() error = %v", err)
		}
		if want := filepath.FromSlash(".buildkite/cache.yml"); got != want {
			t.Errorf("resolveCacheConfigFile() = %q, want %q", got, want)
		}
	})

	t.Run("finds .yaml when only .yaml exists", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".buildkite", "cache.yaml"))
		chdir(t, dir)

		got, err := resolveCacheConfigFile("")
		if err != nil {
			t.Fatalf("resolveCacheConfigFile() error = %v", err)
		}
		if want := filepath.FromSlash(".buildkite/cache.yaml"); got != want {
			t.Errorf("resolveCacheConfigFile() = %q, want %q", got, want)
		}
	})

	t.Run("errors when both .yml and .yaml exist", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".buildkite", "cache.yml"))
		writeFile(t, filepath.Join(dir, ".buildkite", "cache.yaml"))
		chdir(t, dir)

		_, err := resolveCacheConfigFile("")
		if err == nil {
			t.Fatal("resolveCacheConfigFile() error = nil, want error")
		}
	})

	t.Run("errors when neither exists", func(t *testing.T) {
		chdir(t, t.TempDir())

		_, err := resolveCacheConfigFile("")
		if err == nil {
			t.Fatal("resolveCacheConfigFile() error = nil, want error")
		}
	})
}

func TestResolveCacheConfig(t *testing.T) {
	t.Run("path does not require a config file", func(t *testing.T) {
		chdir(t, t.TempDir())

		got, err := resolveCacheConfig(CacheConfig{
			Path:        "~/.npm",
			Registry:    "registry",
			BucketURL:   "s3://cache",
			Names:       []string{"~/.npm"},
			Concurrency: 3,
		})
		if err != nil {
			t.Fatalf("resolveCacheConfig() error = %v, want nil", err)
		}
		if got.Path != "~/.npm" {
			t.Errorf("resolveCacheConfig().Path = %q, want %q", got.Path, "~/.npm")
		}
		if got.CacheConfigFile != "" {
			t.Errorf("resolveCacheConfig().CacheConfigFile = %q, want empty", got.CacheConfigFile)
		}
		if got.Registry != "registry" || got.BucketURL != "s3://cache" || got.Concurrency != 3 {
			t.Errorf("resolveCacheConfig() did not preserve shared options: %+v", got)
		}
	})

	t.Run("path takes precedence over discovered default files", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".buildkite", "cache.yml"))
		writeFile(t, filepath.Join(dir, ".buildkite", "cache.yaml"))
		chdir(t, dir)

		got, err := resolveCacheConfig(CacheConfig{Path: "~/.npm"})
		if err != nil {
			t.Fatalf("resolveCacheConfig() error = %v, want nil", err)
		}
		if got.Path != "~/.npm" || got.CacheConfigFile != "" {
			t.Fatalf("resolveCacheConfig() = %+v, want path configuration", got)
		}
	})

	t.Run("explicit config file and path are mutually exclusive", func(t *testing.T) {
		_, err := resolveCacheConfig(CacheConfig{
			Path:            "~/.npm",
			CacheConfigFile: "cache.yml",
		})
		if err == nil {
			t.Fatal("resolveCacheConfig() error = nil, want error")
		}
	})
}

// writeFile creates an empty file at path, including any parent directories.
func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("os.MkdirAll(%q) = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("os.WriteFile(%q) = %v", path, err)
	}
}

// chdir changes into dir for the duration of the test, restoring the previous
// working directory on cleanup.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() = %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir(%q) = %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prev); err != nil {
			t.Errorf("os.Chdir(%q) = %v", prev, err)
		}
	})
}
