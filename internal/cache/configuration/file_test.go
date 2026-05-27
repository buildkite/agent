package configuration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempCacheFile(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "cache.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q, ...) error = %v, want nil", path, err)
	}
	return path
}

func TestLoadFile_Valid(t *testing.T) {
	t.Parallel()

	config := `dependencies:
  - id: node
    key: 'node-{{ checksum "package-lock.json" }}'
    paths:
      - node_modules
  - id: ruby
    key: 'ruby-{{ checksum "Gemfile.lock" }}'
    paths:
      - vendor/bundle
`
	path := writeTempCacheFile(t, config)

	caches, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile(%q) error = %v, want nil", path, err)
	}
	if got, want := len(caches), 2; got != want {
		t.Fatalf("len(caches) = %d, want %d", got, want)
	}
	if got, want := caches[0].ID, "node"; got != want {
		t.Fatalf("caches[0].ID = %q, want %q", got, want)
	}
	if got, want := caches[1].ID, "ruby"; got != want {
		t.Fatalf("caches[1].ID = %q, want %q", got, want)
	}
}

func TestLoadFile_InvalidYAML(t *testing.T) {
	t.Parallel()

	config := `dependencies:
  - id: node
    key: test
    paths
      - invalid indentation here
    : wrong syntax
`
	path := writeTempCacheFile(t, config)

	_, err := LoadFile(path)
	if err == nil {
		t.Fatalf("LoadFile(%q) error = nil, want non-nil error", path)
	}
	if want := "failed to unmarshal cache config file"; !strings.Contains(err.Error(), want) {
		t.Fatalf("LoadFile(%q) error = %v, want error containing %q", path, err, want)
	}
}

func TestLoadFile_FileNotFound(t *testing.T) {
	t.Parallel()

	missing := "/nonexistent/path/to/cache.yml"
	_, err := LoadFile(missing)
	if err == nil {
		t.Fatalf("LoadFile(%q) error = nil, want non-nil error", missing)
	}
	if want := "failed to read cache config file"; !strings.Contains(err.Error(), want) {
		t.Fatalf("LoadFile(%q) error = %v, want error containing %q", missing, err, want)
	}
}

func TestLoadFile_EmptyFile(t *testing.T) {
	t.Parallel()

	path := writeTempCacheFile(t, "")

	caches, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile(%q) error = %v, want nil", path, err)
	}
	if got := len(caches); got != 0 {
		t.Fatalf("len(caches) = %d, want 0", got)
	}
}
