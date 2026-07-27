package archive

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/cache/internal/trace"
	"github.com/klauspost/compress/zip"
)

func TestChecksumSHA256_Sum(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		input    []byte
	}{
		{
			name:     "empty input",
			input:    []byte{},
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:     "simple string",
			input:    []byte("hello"),
			expected: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		},
		{
			name:     "binary data",
			input:    []byte{0xFF, 0x00, 0xAB, 0xCD},
			expected: "064145b73178d7c9fee36e70bb497d618fadb0e8a7f30b8fe7d9761ef1be635c",
		},
		{
			name:     "unicode string",
			input:    []byte("Hello 世界"),
			expected: "4487dd5e89032c1794903afe6f4b90aaab69972697ea5d3baa215df27c679803",
		},
		{
			name:     "long input",
			input:    bytes.Repeat([]byte("a"), 1000),
			expected: "41edece42d63e8d9bf515a9ba6932e1c20cbc9f5a5d134645adb5db1b9737ea3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ChecksumSHA256{
				sha256: sha256.New(),
				f:      &bytes.Buffer{},
			}
			_, err := c.Write(tt.input)
			if err != nil {
				t.Fatalf("Write: %v", err)
			}
			result := c.Sum()
			if result != tt.expected {
				t.Errorf("Sum() = %v, want %v", result, tt.expected)
			}
		})
	}
}

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

// TestV2_AbsolutePathRoundTrip covers absolute paths — the case v1 could not
// handle (quickzip cannot chroot at a bare root) and the reason this change
// unblocks A-1533. Runs on Windows too: the anchor is the drive root there.
func TestV2_AbsolutePathRoundTrip(t *testing.T) {
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

	if got := readArchiveManifest(t, info).Mappings["_0"]; !isRootAnchor(got) {
		t.Errorf("manifest _0 anchor = %q, want a root anchor", got)
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
		t.Skip("creating a directory symlink needs privileges on Windows")
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
// leading "~" is portable. A pinned path must restore to its exact
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

// TestV2_OverlappingTargetsRejected ensures a target path nested inside another
// is rejected at save, rather than archiving the shared files twice and racing
// on the same destination at restore.
func TestV2_OverlappingTargetsRejected(t *testing.T) {
	mustTrace(t)

	home := t.TempDir()
	setHomeDir(t, home)
	writeTestFile(t, filepath.Join(home, ".cache", "sub", "x.bin"), "data")

	cases := [][]string{
		{"~/.cache", "~/.cache/sub"},                // nested
		{"~/.cache/sub", "~/.cache"},                // nested, order reversed
		{"~/.cache", filepath.Join(home, ".cache")}, // same dir, different spellings
	}

	for _, paths := range cases {
		_, err := BuildArchive(t.Context(), paths, "overlap")
		if err == nil {
			t.Errorf("BuildArchive(%v): expected overlap error, got nil", paths)
			continue
		}
		if !strings.Contains(err.Error(), "overlap") {
			t.Errorf("BuildArchive(%v): error %q should mention overlap", paths, err.Error())
		}
	}
}

// TestV2_OverlappingTargetsViaSymlinkRejected covers overlaps that are only
// visible after resolving symlinks: "real/sub" and "alias/sub" (alias -> real)
// are lexically different but the same physical files, so they must still be
// rejected.
func TestV2_OverlappingTargetsViaSymlinkRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a symlink needs privileges on Windows")
	}
	mustTrace(t)
	setHomeDir(t, t.TempDir())

	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	writeTestFile(t, filepath.Join(realDir, "sub", "x.bin"), "data")

	alias := filepath.Join(base, "alias")
	if err := os.Symlink(realDir, alias); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	// real/sub and alias/sub resolve to the same physical directory.
	_, err := BuildArchive(t.Context(), []string{
		filepath.Join(realDir, "sub"),
		filepath.Join(alias, "sub"),
	}, "overlap-symlink")
	if err == nil {
		t.Fatal("expected overlap error for symlinked-equal targets, got nil")
	}
	if !strings.Contains(err.Error(), "overlap") {
		t.Errorf("error %q should mention overlap", err.Error())
	}
}

// TestPathsOverlap covers the case-sensitivity switch of the overlap
// comparison portably (the case-insensitive branch is what Windows uses).
func TestPathsOverlap(t *testing.T) {
	sep := string(filepath.Separator)
	a := filepath.Join(sep, "a", "Cache")
	lower := filepath.Join(sep, "a", "cache")
	nested := filepath.Join(sep, "a", "x", "y")
	parent := filepath.Join(sep, "a", "x")
	sibling := filepath.Join(sep, "a", "z")

	tests := []struct {
		name            string
		a, b            string
		caseInsensitive bool
		want            bool
	}{
		{"case variant, case-sensitive", a, lower, false, false},
		{"case variant, case-insensitive", a, lower, true, true},
		{"nested", parent, nested, false, true},
		{"nested reversed", nested, parent, false, true},
		{"siblings", parent, sibling, false, false},
	}
	for _, tt := range tests {
		if got := pathsOverlap(tt.a, tt.b, tt.caseInsensitive); got != tt.want {
			t.Errorf("%s: pathsOverlap(%q,%q,%v) = %v, want %v", tt.name, tt.a, tt.b, tt.caseInsensitive, got, tt.want)
		}
	}
}

// TestV2_OverlappingTargetsCaseVariant covers case-only path differences.
// On a case-insensitive filesystem (Windows, a default macOS volume) "Cache"
// and "cache" are the same directory and must be rejected as overlapping; on a
// case-sensitive filesystem they are distinct. The test detects which it's on
// and asserts accordingly, so it exercises the real behaviour on any host.
func TestV2_OverlappingTargetsCaseVariant(t *testing.T) {
	mustTrace(t)
	setHomeDir(t, t.TempDir())

	dir := t.TempDir()
	upper := filepath.Join(dir, "Cache")
	lower := filepath.Join(dir, "cache")
	writeTestFile(t, filepath.Join(upper, "x.bin"), "data")

	// Independently detect case-insensitivity: does "cache" name the same file
	// as the "Cache" we created?
	caseInsensitive := false
	if a, e1 := os.Stat(upper); e1 == nil {
		if b, e2 := os.Stat(lower); e2 == nil && os.SameFile(a, b) {
			caseInsensitive = true
		}
	}

	_, err := BuildArchive(t.Context(), []string{upper, lower}, "case")
	if caseInsensitive {
		if err == nil || !strings.Contains(err.Error(), "overlap") {
			t.Errorf("case-insensitive filesystem: expected overlap error, got %v", err)
		}
	} else {
		// "cache" is a distinct, non-existent path — skipped, so no overlap.
		if err != nil {
			t.Errorf("case-sensitive filesystem: unexpected error %v", err)
		}
	}
}

// TestV2_NonOverlappingSymlinkTargetsAccepted guards against over-rejecting: a
// target that is *itself* a symlink (alias -> real) is archived as the symlink,
// not its referent, so it does not overlap with a separate target under the
// referent (real/sub). Overlap detection must resolve symlinked parents but
// preserve a final symlink, so this config is accepted.
func TestV2_NonOverlappingSymlinkTargetsAccepted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a symlink needs privileges on Windows")
	}
	mustTrace(t)
	setHomeDir(t, t.TempDir())

	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	writeTestFile(t, filepath.Join(realDir, "sub", "x.bin"), "data")

	alias := filepath.Join(base, "alias")
	if err := os.Symlink(realDir, alias); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	// "alias" (the symlink itself) and "real/sub" do not overlap on disk.
	info, err := BuildArchive(t.Context(), []string{alias, filepath.Join(realDir, "sub")}, "no-overlap")
	if err != nil {
		t.Fatalf("BuildArchive should accept non-overlapping symlink targets, got: %v", err)
	}
	defer func() { _ = os.Remove(info.ArchivePath) }()
}

// TestV2_OverlappingTargetsLexicalNestedRejected covers a lexical overlap whose
// child resolves elsewhere via a symlink: "cache" and "cache/link/sub" (with
// cache/link -> other) are not canonically nested, but archiving both would
// make restore create cache/link as a directory and then fail to lay down the
// cache/link symlink. The lexical check must still reject it.
func TestV2_OverlappingTargetsLexicalNestedRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a symlink needs privileges on Windows")
	}
	mustTrace(t)
	setHomeDir(t, t.TempDir())

	base := t.TempDir()
	cacheDir := filepath.Join(base, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeTestFile(t, filepath.Join(base, "other", "sub", "x.bin"), "data")
	if err := os.Symlink(filepath.Join(base, "other"), filepath.Join(cacheDir, "link")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	// cache/link/sub is lexically under cache even though it resolves to other/sub.
	_, err := BuildArchive(t.Context(), []string{
		cacheDir,
		filepath.Join(cacheDir, "link", "sub"),
	}, "overlap-lexical")
	if err == nil {
		t.Fatal("expected overlap error for lexically-nested targets, got nil")
	}
	if !strings.Contains(err.Error(), "overlap") {
		t.Errorf("error %q should mention overlap", err.Error())
	}
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
