package archive

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/zstash/internal/trace"
)

func TestBuildArchive(t *testing.T) {
	_, err := trace.NewProvider(context.Background(), "noop", "test", "0.0.1")
	if err != nil {
		t.Fatalf("trace.NewProvider: %v", err)
	}

	home, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	t.Setenv("HOME", home)

	archiveInfo, err := BuildArchive(context.Background(), []string{"testdata"}, "test")
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	if got, want := archiveInfo.Sha256sum, "3f194172652432099ffc528f81ea6ce1687780287cba1d1a9587f5c26c72aeac"; got != want {
		t.Errorf("Sha256sum = %v, want %v", got, want)
	}
	if got, want := archiveInfo.Size, int64(1228); got != want {
		t.Errorf("Size = %v, want %v", got, want)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}
	if home != homeDir {
		t.Errorf("home = %v, want %v", homeDir, home)
	}
}

func TestBuildAndExtractArchive_MultipleHomeDirPaths(t *testing.T) {
	_, err := trace.NewProvider(context.Background(), "noop", "test", "0.0.1")
	if err != nil {
		t.Fatalf("trace.NewProvider: %v", err)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	goBuildDir := filepath.Join(home, ".go-build")
	err = os.MkdirAll(goBuildDir, 0o755)
	if err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	err = os.WriteFile(filepath.Join(goBuildDir, "cache.txt"), []byte("build cache data"), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	goModDir := filepath.Join(home, "go", "pkg", "mod")
	err = os.MkdirAll(goModDir, 0o755)
	if err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	err = os.WriteFile(filepath.Join(goModDir, "module.txt"), []byte("module cache data"), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	paths := []string{
		"~/.go-build",
		"~/go/pkg/mod",
	}

	archiveInfo, err := BuildArchive(context.Background(), paths, "go-cache")
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	if archiveInfo.ArchivePath == "" {
		t.Error("ArchivePath should not be empty")
	}
	if archiveInfo.Size <= 0 {
		t.Errorf("Size = %v, want > 0", archiveInfo.Size)
	}
	if archiveInfo.Sha256sum == "" {
		t.Error("Sha256sum should not be empty")
	}

	defer func() { _ = os.Remove(archiveInfo.ArchivePath) }()

	err = os.RemoveAll(goBuildDir)
	if err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	err = os.RemoveAll(filepath.Join(home, "go"))
	if err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	_, err = os.Stat(goBuildDir)
	if !os.IsNotExist(err) {
		t.Errorf("expected goBuildDir to not exist, err = %v", err)
	}
	_, err = os.Stat(goModDir)
	if !os.IsNotExist(err) {
		t.Errorf("expected goModDir to not exist, err = %v", err)
	}

	zipFile, err := os.Open(archiveInfo.ArchivePath)
	if err != nil {
		t.Fatalf("os.Open: %v", err)
	}
	defer func() { _ = zipFile.Close() }()

	entries, err := ListArchive(context.Background(), zipFile, archiveInfo.Size)
	if err != nil {
		t.Fatalf("ListArchive: %v", err)
	}
	if !slices.Contains(entries, ".go-build/cache.txt") {
		t.Errorf("entries does not contain %q: %v", ".go-build/cache.txt", entries)
	}
	if !slices.Contains(entries, "go/pkg/mod/module.txt") {
		t.Errorf("entries does not contain %q: %v", "go/pkg/mod/module.txt", entries)
	}

	_, err = zipFile.Seek(0, 0)
	if err != nil {
		t.Fatalf("Seek: %v", err)
	}

	extractInfo, err := ExtractFiles(context.Background(), zipFile, archiveInfo.Size, paths)
	if err != nil {
		t.Fatalf("ExtractFiles: %v", err)
	}
	if extractInfo.WrittenEntries <= 0 {
		t.Errorf("WrittenEntries = %v, want > 0", extractInfo.WrittenEntries)
	}

	cacheContent, err := os.ReadFile(filepath.Join(goBuildDir, "cache.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got, want := string(cacheContent), "build cache data"; got != want {
		t.Errorf("cacheContent = %v, want %v", got, want)
	}

	moduleContent, err := os.ReadFile(filepath.Join(goModDir, "module.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got, want := string(moduleContent), "module cache data"; got != want {
		t.Errorf("moduleContent = %v, want %v", got, want)
	}
}

func TestBuildArchive_MissingPathOnFilesystem(t *testing.T) {
	_, err := trace.NewProvider(context.Background(), "noop", "test", "0.0.1")
	if err != nil {
		t.Fatalf("trace.NewProvider: %v", err)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	goBuildDir := filepath.Join(home, ".go-build")
	err = os.MkdirAll(goBuildDir, 0o755)
	if err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	err = os.WriteFile(filepath.Join(goBuildDir, "cache.txt"), []byte("build cache data"), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	paths := []string{
		"~/.go-build",
		"~/go/pkg/mod",
	}

	archiveInfo, err := BuildArchive(context.Background(), paths, "go-cache")
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	defer func() { _ = os.Remove(archiveInfo.ArchivePath) }()

	zipFile, err := os.Open(archiveInfo.ArchivePath)
	if err != nil {
		t.Fatalf("os.Open: %v", err)
	}
	defer func() { _ = zipFile.Close() }()

	entries, err := ListArchive(context.Background(), zipFile, archiveInfo.Size)
	if err != nil {
		t.Fatalf("ListArchive: %v", err)
	}
	if !slices.Contains(entries, ".go-build/cache.txt") {
		t.Errorf("entries does not contain %q: %v", ".go-build/cache.txt", entries)
	}

	for _, entry := range entries {
		if strings.Contains(entry, "go/pkg/mod") {
			t.Errorf("archive should not contain the missing path, got entry %q", entry)
		}
	}
}

func TestExtractArchive_MissingPathInArchive(t *testing.T) {
	_, err := trace.NewProvider(context.Background(), "noop", "test", "0.0.1")
	if err != nil {
		t.Fatalf("trace.NewProvider: %v", err)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	goBuildDir := filepath.Join(home, ".go-build")
	err = os.MkdirAll(goBuildDir, 0o755)
	if err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	err = os.WriteFile(filepath.Join(goBuildDir, "cache.txt"), []byte("build cache data"), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	archiveInfo, err := BuildArchive(context.Background(), []string{"~/.go-build"}, "go-cache")
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	defer func() { _ = os.Remove(archiveInfo.ArchivePath) }()

	err = os.RemoveAll(goBuildDir)
	if err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	zipFile, err := os.Open(archiveInfo.ArchivePath)
	if err != nil {
		t.Fatalf("os.Open: %v", err)
	}
	defer func() { _ = zipFile.Close() }()

	entries, err := ListArchive(context.Background(), zipFile, archiveInfo.Size)
	if err != nil {
		t.Fatalf("ListArchive: %v", err)
	}
	if !slices.Contains(entries, ".go-build/cache.txt") {
		t.Errorf("entries does not contain %q: %v", ".go-build/cache.txt", entries)
	}
	if slices.Contains(entries, "go/pkg/mod/") {
		t.Errorf("entries should not contain %q: %v", "go/pkg/mod/", entries)
	}

	_, err = zipFile.Seek(0, 0)
	if err != nil {
		t.Fatalf("Seek: %v", err)
	}

	pathsWithMissing := []string{
		"~/.go-build",
		"~/go/pkg/mod",
	}

	extractInfo, err := ExtractFiles(context.Background(), zipFile, archiveInfo.Size, pathsWithMissing)
	if err != nil {
		t.Fatalf("ExtractFiles: %v", err)
	}
	if extractInfo.WrittenEntries <= 0 {
		t.Errorf("WrittenEntries = %v, want > 0", extractInfo.WrittenEntries)
	}

	cacheContent, err := os.ReadFile(filepath.Join(goBuildDir, "cache.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got, want := string(cacheContent), "build cache data"; got != want {
		t.Errorf("cacheContent = %v, want %v", got, want)
	}

	goModDir := filepath.Join(home, "go", "pkg", "mod")
	_, err = os.Stat(goModDir)
	if !os.IsNotExist(err) {
		t.Errorf("go/pkg/mod should not exist since it wasn't in the archive, err = %v", err)
	}
}
