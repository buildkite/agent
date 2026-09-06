package clicommand

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/buildkite/agent/v4/internal/cache/configuration"
	"github.com/google/go-cmp/cmp"
)

func TestResolveCacheConfiguration(t *testing.T) {
	t.Run("path builds an in-memory cache and skips default file discovery", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".buildkite", "cache.yml"))
		writeFile(t, filepath.Join(dir, ".buildkite", "cache.yaml"))
		chdir(t, dir)
		t.Setenv("BUILDKITE_COMMIT", "0123456789abcdef")

		got, err := resolveCacheConfiguration(CacheConfig{Path: "~/.npm"})
		if err != nil {
			t.Fatalf("resolveCacheConfiguration() error = %v", err)
		}

		want := []configuration.Cache{{
			Name: pathCacheName,
			CacheKey: []configuration.KeyPart{
				{Source: configuration.SourceLiteral, Arg: pathCacheFormatVersion},
				{Source: configuration.SourceAgent, Arg: "os"},
				{Source: configuration.SourceAgent, Arg: "arch", FallbackLimit: true},
				{Source: configuration.SourceLiteral, Arg: "0123456789abcdef"},
			},
			TargetPaths: []string{"~/.npm"},
		}}
		if diff := cmp.Diff(got, want); diff != "" {
			t.Fatalf("resolveCacheConfiguration() diff (-got +want):\n%s", diff)
		}
	})

	t.Run("path and explicit config file are mutually exclusive", func(t *testing.T) {
		_, err := resolveCacheConfiguration(CacheConfig{
			Path:            "~/.npm",
			CacheConfigFile: "custom/cache.yml",
		})
		if err == nil {
			t.Fatal("resolveCacheConfiguration() error = nil, want error")
		}
	})

	t.Run("path and name selection are mutually exclusive", func(t *testing.T) {
		_, err := resolveCacheConfiguration(CacheConfig{
			Path:  "~/.npm",
			Names: []string{"npm"},
		})
		if err == nil {
			t.Fatal("resolveCacheConfiguration() error = nil, want error")
		}
	})

	t.Run("explicit config file is loaded", func(t *testing.T) {
		dir := t.TempDir()
		configFile := filepath.Join(dir, "cache.yml")
		if err := os.WriteFile(configFile, []byte(`caches:
  - name: npm
    cache_key: [npm-v1]
    target_paths: [~/.npm]
`), 0o600); err != nil {
			t.Fatalf("os.WriteFile() error = %v", err)
		}

		got, err := resolveCacheConfiguration(CacheConfig{CacheConfigFile: configFile})
		if err != nil {
			t.Fatalf("resolveCacheConfiguration() error = %v", err)
		}
		if len(got) != 1 || got[0].Name != "npm" {
			t.Fatalf("resolveCacheConfiguration() = %#v, want npm cache", got)
		}
	})
}

func TestResolvePathCacheGeneration(t *testing.T) {
	t.Run("uses a concrete Buildkite commit without invoking git", func(t *testing.T) {
		got, err := resolvePathCacheGeneration("0123456789abcdef", func() (string, error) {
			t.Fatal("git should not be invoked")
			return "", nil
		})
		if err != nil {
			t.Fatalf("resolvePathCacheGeneration() error = %v", err)
		}
		if want := "0123456789abcdef"; got != want {
			t.Fatalf("resolvePathCacheGeneration() = %q, want %q", got, want)
		}
	})

	for _, commit := range []string{"", "HEAD"} {
		t.Run("uses git for Buildkite commit "+commit, func(t *testing.T) {
			got, err := resolvePathCacheGeneration(commit, func() (string, error) {
				return " fedcba9876543210\n", nil
			})
			if err != nil {
				t.Fatalf("resolvePathCacheGeneration() error = %v", err)
			}
			if want := "fedcba9876543210"; got != want {
				t.Fatalf("resolvePathCacheGeneration() = %q, want %q", got, want)
			}
		})
	}

	t.Run("errors when no checkout commit is available", func(t *testing.T) {
		_, err := resolvePathCacheGeneration("HEAD", func() (string, error) {
			return "", errors.New("not a git repository")
		})
		if err == nil {
			t.Fatal("resolvePathCacheGeneration() error = nil, want error")
		}
	})
}

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
