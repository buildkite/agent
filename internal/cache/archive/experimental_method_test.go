package archive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildArchiveWithMethod_CheckEntryMethod(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)
	cacheDir := filepath.Join(home, "cache")
	writeTestFile(t, filepath.Join(cacheDir, "a.txt"), strings.Repeat("cache payload ", 1_000))
	writeTestFile(t, filepath.Join(cacheDir, "nested", "b.txt"), strings.Repeat("more payload ", 1_000))
	writeTestFile(t, filepath.Join(cacheDir, "empty.txt"), "")

	for _, method := range []string{"zstd", "store"} {
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

			other := map[string]string{"zstd": "store", "store": "zstd"}[method]
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
