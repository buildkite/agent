package archive

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/internal/cache/internal/trace"
)

func TestArchiveLayoutRootAnchor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX \"/\" path literals; Windows root anchors are drive roots")
	}

	// A child of "/" chroots at the volume root like any other anchor (quickzip
	// >= v1.0.3 can chroot at a bare root, so entries come out as
	// "<namespace>/<path under root>/..." with no special-casing here).
	tests := []struct {
		resolved   string
		namespace  string
		wantPrefix string
	}{
		{"/cache-file", "_0", "_0/"},
		{"/opt/cache", "_1", "_1/"},
	}
	for _, tt := range tests {
		chroot, prefix := (Mapping{Namespace: tt.namespace, Anchor: "/", base: "/", resolved: tt.resolved}).archiveLayout()
		if chroot != "/" {
			t.Errorf("archiveLayout(%q) chroot = %q, want %q", tt.resolved, chroot, "/")
		}
		if prefix != tt.wantPrefix {
			t.Errorf("archiveLayout(%q) prefix = %q, want %q", tt.resolved, prefix, tt.wantPrefix)
		}
	}
}

// TestPathsToMappingsRejectsVolumeRoot covers the validation moved out of
// archiveLayout: a bare volume/filesystem root target is rejected at resolution time
func TestPathsToMappingsRejectsVolumeRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses the POSIX \"/\" root literal; Windows roots are drive roots")
	}
	if _, err := PathsToMappings([]string{"/"}); err == nil {
		t.Error("expected an error caching the volume/filesystem root")
	}
}

func TestBuildArchive(t *testing.T) {
	if runtime.GOOS == "windows" {
		// The expected sha256/size encode unix file modes and LF line
		// endings, which differ on Windows.
		t.Skip("archive byte layout is platform-specific")
	}

	_, err := trace.NewProvider(t.Context(), "noop", "test", "0.0.1")
	if err != nil {
		t.Fatalf("trace.NewProvider: %v", err)
	}

	home, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	setHomeDir(t, home)

	archiveInfo, err := BuildArchive(t.Context(), discardLogger, []string{"testdata"}, "test")
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	if archiveInfo.Size <= 0 {
		t.Errorf("Size = %v, want > 0", archiveInfo.Size)
	}
	if archiveInfo.Sha256sum == "" {
		t.Error("Sha256sum should not be empty")
	}
	defer func() { _ = os.Remove(archiveInfo.ArchivePath) }()

	// Content-addressing relies on the archive being byte-for-byte
	// deterministic for the same inputs.
	second, err := BuildArchive(t.Context(), discardLogger, []string{"testdata"}, "test")
	if err != nil {
		t.Fatalf("BuildArchive (second): %v", err)
	}
	defer func() { _ = os.Remove(second.ArchivePath) }()
	if archiveInfo.Sha256sum != second.Sha256sum {
		t.Errorf("archive is not deterministic: %v != %v", archiveInfo.Sha256sum, second.Sha256sum)
	}

	zipFile, err := os.Open(archiveInfo.ArchivePath)
	if err != nil {
		t.Fatalf("os.Open: %v", err)
	}
	defer func() { _ = zipFile.Close() }()

	entries, err := ListArchive(t.Context(), zipFile, archiveInfo.Size)
	if err != nil {
		t.Fatalf("ListArchive: %v", err)
	}
	if !slices.Contains(entries, ManifestPath) {
		t.Errorf("entries does not contain manifest %q: %v", ManifestPath, entries)
	}
	// "testdata" is a relative path, so it is namespaced under _0 with the "."
	// anchor.
	foundNamespaced := false
	for _, e := range entries {
		if strings.HasPrefix(e, "_0/testdata/") {
			foundNamespaced = true
			break
		}
	}
	if !foundNamespaced {
		t.Errorf("entries does not contain any _0/testdata/ entry: %v", entries)
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
	_, err := trace.NewProvider(t.Context(), "noop", "test", "0.0.1")
	if err != nil {
		t.Fatalf("trace.NewProvider: %v", err)
	}

	home := t.TempDir()
	setHomeDir(t, home)

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

	archiveInfo, err := BuildArchive(t.Context(), discardLogger, paths, "go-cache")
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

	entries, err := ListArchive(t.Context(), zipFile, archiveInfo.Size)
	if err != nil {
		t.Fatalf("ListArchive: %v", err)
	}
	if !slices.Contains(entries, "_0/.go-build/cache.txt") {
		t.Errorf("entries does not contain %q: %v", "_0/.go-build/cache.txt", entries)
	}
	if !slices.Contains(entries, "_1/go/pkg/mod/module.txt") {
		t.Errorf("entries does not contain %q: %v", "_1/go/pkg/mod/module.txt", entries)
	}

	_, err = zipFile.Seek(0, 0)
	if err != nil {
		t.Fatalf("Seek: %v", err)
	}

	extractInfo, err := ExtractFiles(t.Context(), discardLogger, zipFile, archiveInfo.Size, paths)
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
	_, err := trace.NewProvider(t.Context(), "noop", "test", "0.0.1")
	if err != nil {
		t.Fatalf("trace.NewProvider: %v", err)
	}

	home := t.TempDir()
	setHomeDir(t, home)

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

	archiveInfo, err := BuildArchive(t.Context(), discardLogger, paths, "go-cache")
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	defer func() { _ = os.Remove(archiveInfo.ArchivePath) }()

	zipFile, err := os.Open(archiveInfo.ArchivePath)
	if err != nil {
		t.Fatalf("os.Open: %v", err)
	}
	defer func() { _ = zipFile.Close() }()

	entries, err := ListArchive(t.Context(), zipFile, archiveInfo.Size)
	if err != nil {
		t.Fatalf("ListArchive: %v", err)
	}
	if !slices.Contains(entries, "_0/.go-build/cache.txt") {
		t.Errorf("entries does not contain %q: %v", "_0/.go-build/cache.txt", entries)
	}

	for _, entry := range entries {
		if strings.Contains(entry, "go/pkg/mod") {
			t.Errorf("archive should not contain the missing path, got entry %q", entry)
		}
	}
}

func TestExtractArchive_MissingPathInArchive(t *testing.T) {
	_, err := trace.NewProvider(t.Context(), "noop", "test", "0.0.1")
	if err != nil {
		t.Fatalf("trace.NewProvider: %v", err)
	}

	home := t.TempDir()
	setHomeDir(t, home)

	goBuildDir := filepath.Join(home, ".go-build")
	err = os.MkdirAll(goBuildDir, 0o755)
	if err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	err = os.WriteFile(filepath.Join(goBuildDir, "cache.txt"), []byte("build cache data"), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	archiveInfo, err := BuildArchive(t.Context(), discardLogger, []string{"~/.go-build"}, "go-cache")
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

	entries, err := ListArchive(t.Context(), zipFile, archiveInfo.Size)
	if err != nil {
		t.Fatalf("ListArchive: %v", err)
	}
	if !slices.Contains(entries, "_0/.go-build/cache.txt") {
		t.Errorf("entries does not contain %q: %v", "_0/.go-build/cache.txt", entries)
	}
	if slices.Contains(entries, "_0/go/pkg/mod/") {
		t.Errorf("entries should not contain %q: %v", "_0/go/pkg/mod/", entries)
	}

	_, err = zipFile.Seek(0, 0)
	if err != nil {
		t.Fatalf("Seek: %v", err)
	}

	pathsWithMissing := []string{
		"~/.go-build",
		"~/go/pkg/mod",
	}

	extractInfo, err := ExtractFiles(t.Context(), discardLogger, zipFile, archiveInfo.Size, pathsWithMissing)
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
