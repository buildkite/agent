package archive

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/buildkite/agent/v3/internal/cache/internal/trace"
	"github.com/klauspost/compress/zip"
)

func mustTrace(t *testing.T) {
	t.Helper()
	if _, err := trace.NewProvider(t.Context(), "noop", "test", "0.0.1"); err != nil {
		t.Fatalf("trace.NewProvider: %v", err)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func openArchive(t *testing.T, info *ArchiveInfo) *os.File {
	t.Helper()
	f, err := os.Open(info.ArchivePath)
	if err != nil {
		t.Fatalf("os.Open: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func readArchiveManifest(t *testing.T, info *ArchiveInfo) Manifest {
	t.Helper()
	f := openArchive(t, info)
	reader, err := zip.NewReader(f, info.Size)
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	manifest, err := readManifest(reader)
	if err != nil {
		t.Fatalf("readManifest: %v", err)
	}
	return manifest
}

// TestV2_AbsolutePathRoundTrip covers absolute paths outside $HOME — the case
// v1 could not handle (quickzip cannot chroot at "/") and the reason this
// change unblocks A-1533.
func TestV2_AbsolutePathRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Absolute-path (root) anchoring is POSIX-only: a single "/" anchor
		// cannot represent Windows volumes. See saveLayout.
		t.Skip("absolute target_paths are not supported on Windows")
	}

	mustTrace(t)
	setHomeDir(t, t.TempDir())

	absDir := t.TempDir()
	writeTestFile(t, filepath.Join(absDir, "a.txt"), "hello")
	writeTestFile(t, filepath.Join(absDir, "sub", "b.txt"), "world")

	info, err := BuildArchive(t.Context(), []string{absDir}, "abs")
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	defer func() { _ = os.Remove(info.ArchivePath) }()

	if got := readArchiveManifest(t, info).Mappings["_0"]; got != AnchorRoot {
		t.Errorf("manifest _0 anchor = %q, want %q", got, AnchorRoot)
	}

	// Wipe the source and restore it back into the same absolute location.
	if err := os.RemoveAll(absDir); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	if _, err := ExtractFiles(t.Context(), openArchive(t, info), info.Size, []string{absDir}); err != nil {
		t.Fatalf("ExtractFiles: %v", err)
	}

	assertFileContent(t, filepath.Join(absDir, "a.txt"), "hello")
	assertFileContent(t, filepath.Join(absDir, "sub", "b.txt"), "world")
}

// TestV2_EmptyAbsoluteDirectoryRestored checks that an empty root-anchored
// directory survives a round trip — the chroot-root entry must be retained, not
// dropped, or restore recreates nothing.
func TestV2_EmptyAbsoluteDirectoryRestored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("absolute (root-anchored) target_paths are not supported on Windows")
	}
	mustTrace(t)
	setHomeDir(t, t.TempDir())

	absDir := t.TempDir() // absolute, outside home, empty

	info, err := BuildArchive(t.Context(), []string{absDir}, "empty")
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	defer func() { _ = os.Remove(info.ArchivePath) }()

	if err := os.RemoveAll(absDir); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	if _, err := ExtractFiles(t.Context(), openArchive(t, info), info.Size, []string{absDir}); err != nil {
		t.Fatalf("ExtractFiles: %v", err)
	}

	fi, err := os.Stat(absDir)
	if err != nil {
		t.Fatalf("empty directory should be recreated: %v", err)
	}
	if !fi.IsDir() {
		t.Errorf("%q should be a directory", absDir)
	}
}

// TestV2_DirectorySymlinkRestored checks that an absolute target that is a
// symlink to a directory is retained as a symlink rather than dropped.
func TestV2_DirectorySymlinkRestored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("absolute (root-anchored) target_paths are not supported on Windows")
	}
	mustTrace(t)
	setHomeDir(t, t.TempDir())

	realDir := t.TempDir()
	writeTestFile(t, filepath.Join(realDir, "f.txt"), "hi")

	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	info, err := BuildArchive(t.Context(), []string{link}, "symlink")
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	defer func() { _ = os.Remove(info.ArchivePath) }()

	if err := os.Remove(link); err != nil {
		t.Fatalf("Remove link: %v", err)
	}

	if _, err := ExtractFiles(t.Context(), openArchive(t, info), info.Size, []string{link}); err != nil {
		t.Fatalf("ExtractFiles: %v", err)
	}

	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("symlink should be recreated: %v", err)
	}
	if target != realDir {
		t.Errorf("symlink target = %q, want %q", target, realDir)
	}
}

// TestV2_AbsolutePathUnderHomeIsPinned checks that an absolute path that
// happens to sit under $HOME is *pinned* (root-anchored), not portable: only a
// leading "~" is portable per A-1584. A pinned path must restore to its exact
// original location even after $HOME changes, rather than following the new
// home (which would silently drop the entries).
func TestV2_AbsolutePathUnderHomeIsPinned(t *testing.T) {
	mustTrace(t)

	homeA := t.TempDir()
	setHomeDir(t, homeA)

	// Configure the path as an absolute path under home (not "~/...").
	absUnderHome := filepath.Join(homeA, ".gradle")
	writeTestFile(t, filepath.Join(absUnderHome, "caches", "x.bin"), "data")

	info, err := BuildArchive(t.Context(), []string{absUnderHome}, "gradle")
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	defer func() { _ = os.Remove(info.ArchivePath) }()

	anchor := readArchiveManifest(t, info).Mappings["_0"]
	if anchor == AnchorHome {
		t.Errorf("absolute path under $HOME must be pinned, got anchor %q (AnchorHome)", anchor)
	}
	if !isRootAnchor(anchor) {
		t.Errorf("expected a root anchor, got %q", anchor)
	}

	// Change $HOME. The pinned path must still restore to its ORIGINAL absolute
	// location (under homeA), not the new home.
	setHomeDir(t, t.TempDir())

	if err := os.RemoveAll(absUnderHome); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	if _, err := ExtractFiles(t.Context(), openArchive(t, info), info.Size, []string{absUnderHome}); err != nil {
		t.Fatalf("ExtractFiles: %v", err)
	}

	assertFileContent(t, filepath.Join(absUnderHome, "caches", "x.bin"), "data")
}

// TestV2_HomePortability proves a "~" cache saved under one home directory
// restores under a different home directory.
func TestV2_HomePortability(t *testing.T) {
	mustTrace(t)

	homeA := t.TempDir()
	setHomeDir(t, homeA)
	writeTestFile(t, filepath.Join(homeA, ".cache", "x.txt"), "data")

	info, err := BuildArchive(t.Context(), []string{"~/.cache"}, "home")
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	defer func() { _ = os.Remove(info.ArchivePath) }()

	if got := readArchiveManifest(t, info).Mappings["_0"]; got != AnchorHome {
		t.Errorf("manifest _0 anchor = %q, want %q", got, AnchorHome)
	}

	// Restore into a different home directory.
	homeB := t.TempDir()
	setHomeDir(t, homeB)

	if _, err := ExtractFiles(t.Context(), openArchive(t, info), info.Size, []string{"~/.cache"}); err != nil {
		t.Fatalf("ExtractFiles: %v", err)
	}

	assertFileContent(t, filepath.Join(homeB, ".cache", "x.txt"), "data")
}

// TestV2_ExtractMatchesSpellingVariantsAndSkipsUnconfigured checks that restore
// matches on the normalised resolved path (so "~/.cache" matches a config that
// spells it as an absolute path) and skips namespaces absent from local config.
func TestV2_ExtractMatchesSpellingVariantsAndSkipsUnconfigured(t *testing.T) {
	mustTrace(t)

	home := t.TempDir()
	setHomeDir(t, home)
	writeTestFile(t, filepath.Join(home, ".cache", "keep.txt"), "keep")
	writeTestFile(t, filepath.Join(home, ".other", "drop.txt"), "drop")

	info, err := BuildArchive(t.Context(), []string{"~/.cache", "~/.other"}, "multi")
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	defer func() { _ = os.Remove(info.ArchivePath) }()

	if err := os.RemoveAll(filepath.Join(home, ".cache")); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(home, ".other")); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	// Configure only .cache, spelled as an absolute path rather than "~/.cache".
	if _, err := ExtractFiles(t.Context(), openArchive(t, info), info.Size, []string{filepath.Join(home, ".cache")}); err != nil {
		t.Fatalf("ExtractFiles: %v", err)
	}

	assertFileContent(t, filepath.Join(home, ".cache", "keep.txt"), "keep")

	if _, err := os.Stat(filepath.Join(home, ".other", "drop.txt")); !os.IsNotExist(err) {
		t.Errorf(".other should not be restored (not in local config), stat err = %v", err)
	}
}

// TestV2_UnrecognizedFormatSoftFails ensures a v1-style archive (no manifest)
// is detected and reported as ErrUnrecognizedFormat rather than extracted.
func TestV2_UnrecognizedFormatSoftFails(t *testing.T) {
	mustTrace(t)

	tmp, err := os.CreateTemp(t.TempDir(), "v1-*.zip")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer func() { _ = tmp.Close() }()

	zw := zip.NewWriter(tmp)
	w, err := zw.Create("cache/file.txt")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := w.Write([]byte("legacy")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	stat, err := tmp.Stat()
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if err := DetectFormat(tmp, stat.Size()); !errors.Is(err, ErrUnrecognizedFormat) {
		t.Errorf("DetectFormat err = %v, want ErrUnrecognizedFormat", err)
	}

	if _, err := ExtractFiles(t.Context(), tmp, stat.Size(), []string{"cache"}); !errors.Is(err, ErrUnrecognizedFormat) {
		t.Errorf("ExtractFiles err = %v, want ErrUnrecognizedFormat", err)
	}
}

// TestV2_UnrecognizedAnchorSoftFails ensures a manifest whose anchor value
// falls outside {~, /, .} soft-fails as ErrUnrecognizedFormat, the same as an
// absent manifest or bad version, instead of causing a hard failure partway
// through extraction.
func TestV2_UnrecognizedAnchorSoftFails(t *testing.T) {
	mustTrace(t)

	tmp, err := os.CreateTemp(t.TempDir(), "bad-anchor-*.zip")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer func() { _ = tmp.Close() }()

	zw := zip.NewWriter(tmp)
	if err := writeManifest(zw, Manifest{
		Version:  ManifestVersion,
		Mappings: map[string]string{"_0": "?"},
	}); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	stat, err := tmp.Stat()
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if err := DetectFormat(tmp, stat.Size()); !errors.Is(err, ErrUnrecognizedFormat) {
		t.Errorf("DetectFormat err = %v, want ErrUnrecognizedFormat", err)
	}

	if _, err := ExtractFiles(t.Context(), tmp, stat.Size(), []string{"cache"}); !errors.Is(err, ErrUnrecognizedFormat) {
		t.Errorf("ExtractFiles err = %v, want ErrUnrecognizedFormat", err)
	}
}

// TestV2_AbsoluteFileRoundTrip covers a root-anchored target that is a single
// file (not a directory) — its chroot-root entry must round-trip as a file.
func TestV2_AbsoluteFileRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("absolute (root-anchored) target_paths are not supported on Windows")
	}
	mustTrace(t)
	setHomeDir(t, t.TempDir())

	file := filepath.Join(t.TempDir(), "cache-file") // absolute, outside home
	writeTestFile(t, file, "payload")

	info, err := BuildArchive(t.Context(), []string{file}, "absfile")
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	defer func() { _ = os.Remove(info.ArchivePath) }()

	if got := readArchiveManifest(t, info).Mappings["_0"]; got != AnchorRoot {
		t.Errorf("manifest _0 anchor = %q, want %q", got, AnchorRoot)
	}

	if err := os.Remove(file); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, err := ExtractFiles(t.Context(), openArchive(t, info), info.Size, []string{file}); err != nil {
		t.Fatalf("ExtractFiles: %v", err)
	}

	assertFileContent(t, file, "payload")
}

// TestV2_TrailingSlashHomeRoundTrips guards against $HOME carrying a trailing
// slash: anchor bases must be normalised so restore's containment check doesn't
// spuriously reject a valid "~"-anchored entry.
func TestV2_TrailingSlashHomeRoundTrips(t *testing.T) {
	mustTrace(t)

	home := t.TempDir()
	setHomeDir(t, home+string(os.PathSeparator)) // trailing slash
	writeTestFile(t, filepath.Join(home, ".cache", "x.txt"), "data")

	info, err := BuildArchive(t.Context(), []string{"~/.cache"}, "trailing")
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	defer func() { _ = os.Remove(info.ArchivePath) }()

	if err := os.RemoveAll(filepath.Join(home, ".cache")); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	if _, err := ExtractFiles(t.Context(), openArchive(t, info), info.Size, []string{"~/.cache"}); err != nil {
		t.Fatalf("ExtractFiles: %v", err)
	}

	assertFileContent(t, filepath.Join(home, ".cache", "x.txt"), "data")
}

// TestDiscardNameContained ensures discarded entries can never escape the
// throwaway directory, even when the entry name contains "/", "\" or ".."
// segments (a crafted-archive Windows extraction escape).
func TestDiscardNameContained(t *testing.T) {
	discardDir := t.TempDir()

	names := []string{
		".buildkite/cache-manifest.json",
		"../../etc/passwd",
		`..\..\victim`,
		`_9/..\..\..\Windows\System32\evil`,
		"",
		"a/b/c",
	}

	for _, name := range names {
		got := discardName(discardDir, name)
		if !isUnder(got, discardDir) {
			t.Errorf("discardName(%q) = %q, escapes discardDir %q", name, got, discardDir)
		}
		// The mapped name must be a single component directly under discardDir.
		if filepath.Dir(got) != filepath.Clean(discardDir) {
			t.Errorf("discardName(%q) = %q, not a direct child of %q", name, got, discardDir)
		}
	}
}

// TestIsUnder locks the containment logic, including roots that already end in
// a separator (the POSIX root, and — exercised on Windows — a volume root).
func TestIsUnder(t *testing.T) {
	sep := string(filepath.Separator)
	tests := []struct {
		p, root string
		want    bool
	}{
		{filepath.Join(sep, "a", "b"), sep, true}, // separator-terminated root
		{sep, sep, true}, // equal to root
		{filepath.Join(sep, "home", "user", "x"), filepath.Join(sep, "home", "user"), true},
		{filepath.Join(sep, "home", "user2"), filepath.Join(sep, "home", "user"), false}, // prefix sibling, not under
		{filepath.Join(sep, "home", "user"), filepath.Join(sep, "home", "user"), true},
	}
	for _, tt := range tests {
		if got := isUnder(tt.p, tt.root); got != tt.want {
			t.Errorf("isUnder(%q, %q) = %v, want %v", tt.p, tt.root, got, tt.want)
		}
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %q: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("content of %q = %q, want %q", path, string(got), want)
	}
}
