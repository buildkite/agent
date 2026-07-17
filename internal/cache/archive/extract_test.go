package archive

import (
	stdzip "archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/cache/internal/trace"
)

func TestEntryMatchesMapping(t *testing.T) {
	tests := []struct {
		name         string
		entryName    string
		relativePath string
		want         bool
	}{
		{
			name:         "entry under mapping",
			entryName:    "cache/file.txt",
			relativePath: "cache",
			want:         true,
		},
		{
			name:         "entry equals mapping (single-file target)",
			entryName:    "cache",
			relativePath: "cache",
			want:         true,
		},
		{
			name:         "sibling with shared name prefix does not match",
			entryName:    "cache2/file.txt",
			relativePath: "cache",
			want:         false,
		},
		{
			name:         "trailing slash in mapping",
			entryName:    "cache/file.txt",
			relativePath: "cache/",
			want:         true,
		},
		{
			name:         "dot-slash prefix in mapping",
			entryName:    "cache/file.txt",
			relativePath: "./cache",
			want:         true,
		},
		{
			name:         "nested mapping path",
			entryName:    "go/pkg/mod/module.txt",
			relativePath: filepath.Join("go", "pkg", "mod"),
			want:         true,
		},
		{
			name:         "unrelated entry",
			entryName:    "other/file.txt",
			relativePath: "cache",
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := entryMatchesMapping(tt.entryName, tt.relativePath); got != tt.want {
				t.Errorf("entryMatchesMapping(%q, %q) = %v, want %v", tt.entryName, tt.relativePath, got, tt.want)
			}
		})
	}
}

func TestExtractFiles_RejectsEscapingEntries(t *testing.T) {
	_, err := trace.NewProvider(t.Context(), "noop", "test", "0.0.1")
	if err != nil {
		t.Fatalf("trace.NewProvider: %v", err)
	}

	// Craft an archive whose entry matches the "cache" mapping prefix but
	// traverses out of it.
	zipPath := filepath.Join(t.TempDir(), "evil.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("os.Create: %v", err)
	}
	zw := stdzip.NewWriter(f)
	w, err := zw.Create("cache/../../escape.txt")
	if err != nil {
		t.Fatalf("zip Create: %v", err)
	}
	if _, err := w.Write([]byte("escaped")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	zipFile, err := os.Open(zipPath)
	if err != nil {
		t.Fatalf("os.Open: %v", err)
	}
	defer func() { _ = zipFile.Close() }()

	stat, err := zipFile.Stat()
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	_, err = ExtractFiles(t.Context(), zipFile, stat.Size(), []string{"cache"})
	if err == nil {
		t.Fatal("ExtractFiles: expected error for escaping entry, got nil")
	}
	if !strings.Contains(err.Error(), "escapes target path") {
		t.Errorf("error %q should contain %q", err.Error(), "escapes target path")
	}
}
