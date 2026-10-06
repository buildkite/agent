package archive

import (
	"bytes"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
)

func TestBuildArchiveWithMethod_CheckEntryMethod(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)
	cacheDir := filepath.Join(home, "cache")
	writeTestFile(t, filepath.Join(cacheDir, "a.txt"), strings.Repeat("cache payload ", 1_000))
	writeTestFile(t, filepath.Join(cacheDir, "nested", "b.txt"), strings.Repeat("more payload ", 1_000))
	writeTestFile(t, filepath.Join(cacheDir, "empty.txt"), "")

	for _, method := range []string{"zstd", "zstd_parallel", "store"} {
		t.Run(method, func(t *testing.T) {
			info, err := BuildArchiveWithMethod(t.Context(), []string{"~/cache"}, method, method)
			if err != nil {
				t.Fatalf("BuildArchiveWithMethod: %v", err)
			}
			t.Cleanup(func() { _ = os.Remove(info.ArchivePath) })

			// Only the two non-empty files depend on the method.
			checked, err := CheckEntryMethod(info.ArchivePath, info.Size, method)
			if err != nil {
				t.Fatalf("CheckEntryMethod(%s): %v", method, err)
			}
			if checked != 2 {
				t.Errorf("CheckEntryMethod(%s) checked %d entries, want 2", method, checked)
			}

			other := map[string]string{"zstd": "store", "zstd_parallel": "store", "store": "zstd"}[method]
			if _, err := CheckEntryMethod(info.ArchivePath, info.Size, other); err == nil {
				t.Errorf("CheckEntryMethod(%s) on a %s archive succeeded, want an error", other, method)
			}
		})
	}
}

func TestBuildArchiveWithMethod_RejectsUnknownMethod(t *testing.T) {
	if _, err := BuildArchiveWithMethod(t.Context(), []string{"~/cache"}, "deflate", "deflate"); err == nil {
		t.Fatal("BuildArchiveWithMethod(deflate) succeeded, want an error")
	}
	if err := ValidateEntryMethod(""); err == nil {
		t.Fatal("ValidateEntryMethod(\"\") succeeded, want an error")
	}
}

// TestBuildArchiveWithMethod_ZstdParallel checks that parallel encoding of
// entries big enough to split into several jobs (32 MiB each) produces
// ordinary Zstd entries that the unchanged extraction path restores, about
// the same size as the default encoder's.
func TestBuildArchiveWithMethod_ZstdParallel(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)
	cacheDir := filepath.Join(home, "cache")
	rng := rand.New(rand.NewPCG(1, 2))
	want := make([]byte, 100<<20)
	for i := range want {
		want[i] = byte(rng.IntN(8)) // about 2:1 compressible
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "big.bin"), want, 0o600); err != nil {
		t.Fatal(err)
	}

	sizes := map[string]int64{}
	for _, method := range []string{"zstd", "zstd_parallel"} {
		info, err := BuildArchiveWithMethod(t.Context(), []string{"~/cache"}, method, method)
		if err != nil {
			t.Fatalf("BuildArchiveWithMethod(%s): %v", method, err)
		}
		t.Cleanup(func() { _ = os.Remove(info.ArchivePath) })
		sizes[method] = info.Size
		if _, err := CheckEntryMethod(info.ArchivePath, info.Size, "zstd"); err != nil {
			t.Errorf("%s archive: %v", method, err)
		}

		f := openArchive(t, info)
		reader, err := zip.NewReader(f, info.Size)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range reader.File {
			if entry.Name == "_0/cache/big.bin" && entry.CompressedSize64 >= entry.UncompressedSize64 {
				t.Errorf("%s: big.bin compressed to %d of %d bytes", method, entry.CompressedSize64, entry.UncompressedSize64)
			}
		}
		if err := os.RemoveAll(cacheDir); err != nil {
			t.Fatal(err)
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		if _, err := ExtractFiles(t.Context(), f, info.Size, []string{"~/cache"}); err != nil {
			t.Fatalf("ExtractFiles(%s archive): %v", method, err)
		}
		got, err := os.ReadFile(filepath.Join(cacheDir, "big.bin"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: restored big.bin differs from the source", method)
		}
	}
	if ratio := float64(sizes["zstd_parallel"]) / float64(sizes["zstd"]); ratio < 0.95 || ratio > 1.05 {
		t.Errorf("zstd_parallel archive is %.3f× the zstd archive's size, want within 5%%", ratio)
	}
	t.Logf("archive sizes: %v", sizes)
}
