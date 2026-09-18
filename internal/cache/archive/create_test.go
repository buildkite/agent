package archive

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/internal/cache/internal/trace"
	"github.com/klauspost/compress/zip"
)

func TestRemoveZip64Extra(t *testing.T) {
	field := func(tag uint16, data ...byte) []byte {
		b := make([]byte, 4+len(data))
		binary.LittleEndian.PutUint16(b, tag)
		binary.LittleEndian.PutUint16(b[2:], uint16(len(data)))
		copy(b[4:], data)
		return b
	}
	zip64 := field(0x0001, make([]byte, 8)...)
	other1 := field(0x1234, 1, 2, 3)
	other2 := field(0x5678, 4, 5)
	truncated := []byte{0x99, 0x99, 0x04, 0x00, 6, 7}

	tests := []struct {
		name  string
		extra []byte
		want  []byte
	}{
		{name: "ZIP64 only", extra: zip64},
		{name: "mixed fields", extra: append(append(bytes.Clone(other1), zip64...), other2...), want: append(bytes.Clone(other1), other2...)},
		{name: "multiple ZIP64 fields", extra: append(append(bytes.Clone(zip64), other1...), zip64...), want: other1},
		{name: "malformed trailing field", extra: append(append(bytes.Clone(other1), zip64...), truncated...), want: append(bytes.Clone(other1), truncated...)},
		{name: "short trailing bytes", extra: append(append(bytes.Clone(other1), zip64...), 0xaa, 0xbb), want: append(bytes.Clone(other1), 0xaa, 0xbb)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := bytes.Clone(tt.extra)
			got := removeZip64Extra(input)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("removeZip64Extra(%x) = %x, want %x", tt.extra, got, tt.want)
			}
			if got == nil {
				t.Fatal("removeZip64Extra returned nil for non-empty input")
			}
			if len(got) == 0 {
				got = append(got, 0xff)
				if got[0] != 0xff {
					t.Fatal("failed to append to empty result")
				}
			} else {
				got[0] ^= 0xff
			}
			if !bytes.Equal(input, tt.extra) {
				t.Fatal("result shares its backing array with the input")
			}
		})
	}
}

func TestCopyRawFileZip64Offsets(t *testing.T) {
	const destinationOffset = int64(0x100003039)
	payload := []byte("cache data")

	tests := []struct {
		name            string
		sourceOffset    int64
		wantSourceZip64 int
	}{
		{name: "below ZIP64 boundary", sourceOffset: 0xfffffffe, wantSourceZip64: 0},
		{name: "at ZIP64 boundary", sourceOffset: 0xffffffff, wantSourceZip64: 1},
		{name: "above ZIP64 boundary", sourceOffset: 0x100000000, wantSourceZip64: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := writeVirtualOffsetArchive(t, tt.sourceOffset, payload)
			sr, err := zip.NewReader(source, source.size())
			if err != nil {
				t.Fatalf("zip.NewReader(source): %v", err)
			}
			if got := zip64ExtraCount(sr.File[0].Extra); got != tt.wantSourceZip64 {
				t.Fatalf("source ZIP64 extra count = %d, want %d", got, tt.wantSourceZip64)
			}
			sourceExtra := bytes.Clone(sr.File[0].Extra)

			destination := &virtualOffsetBuffer{offset: destinationOffset}
			dw := zip.NewWriter(destination)
			dw.SetOffset(destinationOffset)
			hdr := sr.File[0].FileHeader
			hdr.Name = "copied"
			if err := copyRawFile(dw, sr.File[0], hdr); err != nil {
				t.Fatalf("copyRawFile: %v", err)
			}
			if err := dw.Close(); err != nil {
				t.Fatalf("destination Close: %v", err)
			}
			if !bytes.Equal(sr.File[0].Extra, sourceExtra) {
				t.Fatal("copyRawFile modified the source header Extra")
			}

			dr, err := zip.NewReader(destination, destination.size())
			if err != nil {
				t.Fatalf("zip.NewReader(destination): %v", err)
			}
			if got := zip64ExtraCount(dr.File[0].Extra); got != 1 {
				t.Fatalf("destination ZIP64 extra count = %d, want 1", got)
			}
			if got := zip64Offset(dr.File[0].Extra); got != uint64(destinationOffset) {
				t.Errorf("destination ZIP64 offset = %#x, want %#x", got, destinationOffset)
			}

			r, err := dr.File[0].Open()
			if err != nil {
				t.Fatalf("destination entry Open: %v", err)
			}
			got, err := io.ReadAll(r)
			if err != nil {
				t.Fatalf("destination entry ReadAll: %v", err)
			}
			if err := r.Close(); err != nil {
				t.Fatalf("destination entry Close: %v", err)
			}
			if !bytes.Equal(got, payload) {
				t.Errorf("destination content = %q, want %q", got, payload)
			}
		})
	}
}

type virtualOffsetBuffer struct {
	offset int64
	data   []byte
}

func (b *virtualOffsetBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *virtualOffsetBuffer) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, os.ErrInvalid
	}
	n := 0
	if off < b.offset {
		zeroes := len(p)
		if gap := b.offset - off; gap < int64(zeroes) {
			zeroes = int(gap)
		}
		clear(p[:zeroes])
		n += zeroes
		off += int64(zeroes)
	}
	if n < len(p) && off >= b.offset {
		start := off - b.offset
		if start < int64(len(b.data)) {
			n += copy(p[n:], b.data[start:])
		}
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (b *virtualOffsetBuffer) size() int64 {
	return b.offset + int64(len(b.data))
}

func writeVirtualOffsetArchive(t *testing.T, offset int64, payload []byte) *virtualOffsetBuffer {
	t.Helper()
	b := &virtualOffsetBuffer{offset: offset}
	zw := zip.NewWriter(b)
	zw.SetOffset(offset)
	w, err := zw.Create("source")
	if err != nil {
		t.Fatalf("source Create: %v", err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("source Write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("source Close: %v", err)
	}
	return b
}

func zip64ExtraCount(extra []byte) int {
	count := 0
	for len(extra) >= 4 {
		tag := binary.LittleEndian.Uint16(extra)
		size := int(binary.LittleEndian.Uint16(extra[2:]))
		if size > len(extra)-4 {
			break
		}
		if tag == 0x0001 {
			count++
		}
		extra = extra[4+size:]
	}
	return count
}

func zip64Offset(extra []byte) uint64 {
	for len(extra) >= 4 {
		tag := binary.LittleEndian.Uint16(extra)
		size := int(binary.LittleEndian.Uint16(extra[2:]))
		if size > len(extra)-4 {
			return 0
		}
		if tag == 0x0001 && size >= 24 {
			return binary.LittleEndian.Uint64(extra[20:28])
		}
		extra = extra[4+size:]
	}
	return 0
}

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

	archiveInfo, err := BuildArchive(t.Context(), []string{"testdata"}, "test")
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
	second, err := BuildArchive(t.Context(), []string{"testdata"}, "test")
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

	archiveInfo, err := BuildArchive(t.Context(), paths, "go-cache")
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

	extractInfo, err := ExtractFiles(t.Context(), zipFile, archiveInfo.Size, paths)
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

	archiveInfo, err := BuildArchive(t.Context(), paths, "go-cache")
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

	archiveInfo, err := BuildArchive(t.Context(), []string{"~/.go-build"}, "go-cache")
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

	extractInfo, err := ExtractFiles(t.Context(), zipFile, archiveInfo.Size, pathsWithMissing)
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
