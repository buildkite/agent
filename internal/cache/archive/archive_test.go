package archive

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/internal/cache/internal/trace"
	"github.com/klauspost/compress/zip"
)

var discardLogger = slog.New(slog.DiscardHandler)

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

	info, err := BuildArchive(t.Context(), discardLogger, []string{absDir}, "abs")
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

	if _, err := ExtractFiles(t.Context(), discardLogger, openArchive(t, info), info.Size, []string{absDir}); err != nil {
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

	info, err := BuildArchive(t.Context(), discardLogger, []string{absDir}, "empty")
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	defer func() { _ = os.Remove(info.ArchivePath) }()

	if err := os.RemoveAll(absDir); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	if _, err := ExtractFiles(t.Context(), discardLogger, openArchive(t, info), info.Size, []string{absDir}); err != nil {
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

	info, err := BuildArchive(t.Context(), discardLogger, []string{link}, "symlink")
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	defer func() { _ = os.Remove(info.ArchivePath) }()

	if err := os.Remove(link); err != nil {
		t.Fatalf("Remove link: %v", err)
	}

	if _, err := ExtractFiles(t.Context(), discardLogger, openArchive(t, info), info.Size, []string{link}); err != nil {
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

	info, err := BuildArchive(t.Context(), discardLogger, []string{absUnderHome}, "gradle")
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

	if _, err := ExtractFiles(t.Context(), discardLogger, openArchive(t, info), info.Size, []string{absUnderHome}); err != nil {
		t.Fatalf("ExtractFiles: %v", err)
	}

	assertFileContent(t, filepath.Join(absUnderHome, "caches", "x.bin"), "data")
}

// TestV2_TargetOverlap checks BuildArchive rejects overlapping targets and
// accepts non-overlapping ones across nesting, symlink, and spelling shapes.
func TestV2_TargetOverlap(t *testing.T) {
	mustTrace(t)

	tests := []struct {
		name    string
		symlink bool // needs symlink privileges
		setup   func(t *testing.T, base string) []string
		wantErr bool
	}{
		{
			name: "nested",
			setup: func(t *testing.T, base string) []string {
				writeTestFile(t, filepath.Join(base, "cache", "sub", "x"), "data")
				return []string{filepath.Join(base, "cache"), filepath.Join(base, "cache", "sub")}
			},
			wantErr: true,
		},
		{
			name: "tilde and absolute spelling of one dir",
			setup: func(t *testing.T, base string) []string {
				setHomeDir(t, base)
				writeTestFile(t, filepath.Join(base, "cache", "x"), "data")
				return []string{"~/cache", filepath.Join(base, "cache")}
			},
			wantErr: true,
		},
		{
			name:    "symlink alias to the same dir",
			symlink: true,
			setup: func(t *testing.T, base string) []string {
				real := filepath.Join(base, "real")
				writeTestFile(t, filepath.Join(real, "sub", "x"), "data")
				mustSymlink(t, real, filepath.Join(base, "alias"))
				return []string{filepath.Join(real, "sub"), filepath.Join(base, "alias", "sub")}
			},
			wantErr: true,
		},
		{
			name:    "symlinked parent, lexically nested",
			symlink: true,
			setup: func(t *testing.T, base string) []string {
				cacheDir := filepath.Join(base, "cache")
				if err := os.MkdirAll(cacheDir, 0o755); err != nil {
					t.Fatalf("MkdirAll: %v", err)
				}
				writeTestFile(t, filepath.Join(base, "other", "sub", "x"), "data")
				mustSymlink(t, filepath.Join(base, "other"), filepath.Join(cacheDir, "link"))
				return []string{cacheDir, filepath.Join(cacheDir, "link", "sub")}
			},
			wantErr: true,
		},
		{
			// A final symlink is archived as the link, not its referent.
			name:    "final symlink does not overlap its referent",
			symlink: true,
			setup: func(t *testing.T, base string) []string {
				real := filepath.Join(base, "real")
				writeTestFile(t, filepath.Join(real, "sub", "x"), "data")
				alias := filepath.Join(base, "alias")
				mustSymlink(t, real, alias)
				return []string{alias, filepath.Join(real, "sub")}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.symlink && runtime.GOOS == "windows" {
				t.Skip("creating a symlink needs privileges on Windows")
			}
			paths := tt.setup(t, t.TempDir())
			_, err := BuildArchive(t.Context(), discardLogger, paths, "overlap")
			switch {
			case tt.wantErr && (err == nil || !strings.Contains(err.Error(), "overlap")):
				t.Errorf("BuildArchive(%v) = %v, want overlap error", paths, err)
			case !tt.wantErr && err != nil:
				t.Errorf("BuildArchive(%v) = %v, want no error", paths, err)
			}
		})
	}
}

// mustSymlink creates a symlink or fails the test.
func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink: %v", err)
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

// TestV2_TargetOverlapCaseVariant covers case-only differences via BuildArchive,
// adapting to the host filesystem's case behaviour (existing targets).
func TestV2_TargetOverlapCaseVariant(t *testing.T) {
	mustTrace(t)

	tests := []struct{ name, upper, lower string }{
		{"leaf", "Cache", "cache"},
		{"numeric leaf under a cased parent", filepath.Join("Cache", "2024"), filepath.Join("cache", "2024")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setHomeDir(t, t.TempDir())
			base := t.TempDir()
			upper := filepath.Join(base, tt.upper)
			writeTestFile(t, filepath.Join(upper, "x.bin"), "data")

			_, err := BuildArchive(t.Context(), discardLogger, []string{upper, filepath.Join(base, tt.lower)}, "case")
			if caseInsensitiveFS(t, base) {
				if err == nil || !strings.Contains(err.Error(), "overlap") {
					t.Errorf("case-insensitive: BuildArchive = %v, want overlap error", err)
				}
			} else if err != nil {
				t.Errorf("case-sensitive: BuildArchive = %v, want no error", err)
			}
		})
	}
}

// TestOverlappingPathsAbsentCaseVariant covers a fresh restore: case-variant
// targets that don't exist yet must overlap iff the filesystem folds case.
func TestOverlappingPathsAbsentCaseVariant(t *testing.T) {
	base := t.TempDir()
	want := caseInsensitiveFS(t, base)
	_, _, ok := OverlappingPaths([]string{
		filepath.Join(base, "Cache"),
		filepath.Join(base, "cache"),
	})
	if ok != want {
		t.Errorf("OverlappingPaths overlap = %v, want %v (case-insensitive=%v)", ok, want, want)
	}
}

// TestOverlappingPathsReadOnlyCaseVariant covers existing case-variant targets
// whose destination is read-only (0555), so the writable probe (MkdirTemp)
// fails — file identity must still detect the overlap. The numeric-leaf variant
// exercises re-casing an ancestor rather than the (all-digit) leaf.
func TestOverlappingPathsReadOnlyCaseVariant(t *testing.T) {
	tests := []struct{ name, upper, lower string }{
		{"lettered leaf", "Cache", "cache"},
		{"numeric leaf under a cased parent", filepath.Join("Cache", "123"), filepath.Join("cache", "123")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			if !caseInsensitiveFS(t, base) {
				t.Skip("requires a case-insensitive filesystem")
			}
			upper := filepath.Join(base, tt.upper)
			if err := os.MkdirAll(upper, 0o755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			// Make the target read-only so MkdirTemp inside it fails; detection
			// must fall back to file identity.
			if err := os.Chmod(upper, 0o555); err != nil {
				t.Fatalf("Chmod: %v", err)
			}
			t.Cleanup(func() { _ = os.Chmod(upper, 0o755) })

			if _, _, ok := OverlappingPaths([]string{upper, filepath.Join(base, tt.lower)}); !ok {
				t.Error("case-variant targets in a read-only dir should overlap via identity")
			}
		})
	}
}

// caseInsensitiveFS reports whether dir's filesystem folds case, via a marker.
func caseInsensitiveFS(t *testing.T, dir string) bool {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "Marker"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	a, e1 := os.Stat(filepath.Join(dir, "Marker"))
	b, e2 := os.Stat(filepath.Join(dir, "marker"))
	return e1 == nil && e2 == nil && os.SameFile(a, b)
}

// TestV2_HomePortability proves a "~" cache saved under one home directory
// restores under a different home directory.
func TestV2_HomePortability(t *testing.T) {
	mustTrace(t)

	homeA := t.TempDir()
	setHomeDir(t, homeA)
	writeTestFile(t, filepath.Join(homeA, ".cache", "x.txt"), "data")

	info, err := BuildArchive(t.Context(), discardLogger, []string{"~/.cache"}, "home")
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

	if _, err := ExtractFiles(t.Context(), discardLogger, openArchive(t, info), info.Size, []string{"~/.cache"}); err != nil {
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

	info, err := BuildArchive(t.Context(), discardLogger, []string{"~/.cache", "~/.other"}, "multi")
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
	if _, err := ExtractFiles(t.Context(), discardLogger, openArchive(t, info), info.Size, []string{filepath.Join(home, ".cache")}); err != nil {
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

	if err := Validate(tmp.Name(), stat.Size()); !errors.Is(err, ErrUnrecognizedFormat) {
		t.Errorf("Validate err = %v, want ErrUnrecognizedFormat", err)
	}

	if _, err := ExtractFiles(t.Context(), discardLogger, tmp, stat.Size(), []string{"cache"}); !errors.Is(err, ErrUnrecognizedFormat) {
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

	if err := Validate(tmp.Name(), stat.Size()); !errors.Is(err, ErrUnrecognizedFormat) {
		t.Errorf("Validate err = %v, want ErrUnrecognizedFormat", err)
	}

	if _, err := ExtractFiles(t.Context(), discardLogger, tmp, stat.Size(), []string{"cache"}); !errors.Is(err, ErrUnrecognizedFormat) {
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

	info, err := BuildArchive(t.Context(), discardLogger, []string{file}, "absfile")
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	defer func() { _ = os.Remove(info.ArchivePath) }()

	if got := readArchiveManifest(t, info).Mappings["_0"]; got != "/" {
		t.Errorf("manifest _0 anchor = %q, want %q", got, "/")
	}

	if err := os.Remove(file); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, err := ExtractFiles(t.Context(), discardLogger, openArchive(t, info), info.Size, []string{file}); err != nil {
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

	info, err := BuildArchive(t.Context(), discardLogger, []string{"~/.cache"}, "trailing")
	if err != nil {
		t.Fatalf("BuildArchive: %v", err)
	}
	defer func() { _ = os.Remove(info.ArchivePath) }()

	if err := os.RemoveAll(filepath.Join(home, ".cache")); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	if _, err := ExtractFiles(t.Context(), discardLogger, openArchive(t, info), info.Size, []string{"~/.cache"}); err != nil {
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
