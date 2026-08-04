package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/api"
)

func TestVerifyBlobDigest(t *testing.T) {
	dir := t.TempDir()
	archiveFile := filepath.Join(dir, "archive.zip")
	content := []byte("hello cache archive")
	if err := os.WriteFile(archiveFile, content, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	sum := sha256.Sum256(content)
	digestValue := hex.EncodeToString(sum[:])

	t.Run("matching sha256 passes", func(t *testing.T) {
		err := verifyBlobDigest(context.Background(), archiveFile, api.CacheDigest{Algorithm: "sha256", Value: digestValue})
		if err != nil {
			t.Errorf("verifyBlobDigest = %v, want nil", err)
		}
	})

	t.Run("mismatched sha256 is ErrDigestMismatch", func(t *testing.T) {
		err := verifyBlobDigest(context.Background(), archiveFile, api.CacheDigest{Algorithm: "sha256", Value: "deadbeef"})
		if !errors.Is(err, ErrDigestMismatch) {
			t.Errorf("verifyBlobDigest = %v, want ErrDigestMismatch", err)
		}
	})

	t.Run("unknown algorithm is skipped, not reported corrupt", func(t *testing.T) {
		// A non-sha256 digest we can't verify must pass through, not be flagged as
		// a mismatch — otherwise a future wire format breaks every restore.
		err := verifyBlobDigest(context.Background(), archiveFile, api.CacheDigest{Algorithm: "blake3", Value: "unverifiable"})
		if err != nil {
			t.Errorf("verifyBlobDigest with unknown algorithm = %v, want nil (skip)", err)
		}
	})

	t.Run("cancelled context aborts", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := verifyBlobDigest(ctx, archiveFile, api.CacheDigest{Algorithm: "sha256", Value: digestValue})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("verifyBlobDigest with cancelled ctx = %v, want context.Canceled", err)
		}
	})
}

func TestCleanPath(t *testing.T) {
	t.Run("removes directory and contents", func(t *testing.T) {
		dir := t.TempDir()
		testDir := filepath.Join(dir, "cache")
		if err := os.MkdirAll(filepath.Join(testDir, "subdir"), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(testDir, "file.txt"), []byte("test"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if err := os.WriteFile(filepath.Join(testDir, "subdir", "nested.txt"), []byte("nested"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		if err := cleanPath(t.Context(), testDir); err != nil {
			t.Fatalf("cleanPath: %v", err)
		}

		_, err := os.Stat(testDir)
		if !os.IsNotExist(err) {
			t.Errorf("directory should be removed, got err=%v", err)
		}
	})

	t.Run("handles read-only directories (like go module cache)", func(t *testing.T) {
		dir := t.TempDir()
		testDir := filepath.Join(dir, "modcache")
		subdir := filepath.Join(testDir, "pkg")
		if err := os.MkdirAll(subdir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(subdir, "mod.go"), []byte("package mod"), 0o400); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if err := os.Chmod(subdir, 0o555); err != nil {
			t.Fatalf("Chmod: %v", err)
		}
		if err := os.Chmod(testDir, 0o555); err != nil {
			t.Fatalf("Chmod: %v", err)
		}

		if err := cleanPath(t.Context(), testDir); err != nil {
			t.Fatalf("cleanPath: %v", err)
		}

		_, err := os.Stat(testDir)
		if !os.IsNotExist(err) {
			t.Errorf("directory should be removed, got err=%v", err)
		}
	})

	t.Run("succeeds on non-existent path", func(t *testing.T) {
		if err := cleanPath(t.Context(), "/nonexistent/path/that/does/not/exist"); err != nil {
			t.Fatalf("cleanPath: %v", err)
		}
	})

	t.Run("rejects empty path", func(t *testing.T) {
		err := cleanPath(t.Context(), "")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "empty directory path") {
			t.Errorf("error %q should contain %q", err.Error(), "empty directory path")
		}
	})

	t.Run("rejects root path", func(t *testing.T) {
		err := cleanPath(t.Context(), "/")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "refusing to remove") {
			t.Errorf("error %q should contain %q", err.Error(), "refusing to remove")
		}
	})

	t.Run("rejects current directory", func(t *testing.T) {
		err := cleanPath(t.Context(), ".")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "refusing to remove") {
			t.Errorf("error %q should contain %q", err.Error(), "refusing to remove")
		}
	})

	t.Run("rejects resolved current directory", func(t *testing.T) {
		// target_paths: ["."] resolves to the absolute cwd before cleanup; the
		// guard must catch that too, not just the literal ".".
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("os.Getwd: %v", err)
		}
		err = cleanPath(t.Context(), cwd)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "refusing to remove") {
			t.Errorf("error %q should contain %q", err.Error(), "refusing to remove")
		}
	})

	t.Run("rejects home directory", func(t *testing.T) {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("UserHomeDir: %v", err)
		}

		err = cleanPath(t.Context(), home)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "refusing to remove") {
			t.Errorf("error %q should contain %q", err.Error(), "refusing to remove")
		}
	})

	t.Run("removes a regular file", func(t *testing.T) {
		// A file target must be removable; makeTreeWritable's os.OpenRoot only
		// works on directories, so cleanPath must skip it for files.
		file := filepath.Join(t.TempDir(), "cache-file")
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if err := cleanPath(t.Context(), file); err != nil {
			t.Fatalf("cleanPath: %v", err)
		}
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Errorf("file should be removed, stat err = %v", err)
		}
	})

	t.Run("removes a final symlink without following it to a protected dir", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation needs privileges on Windows")
		}
		// A final symlink is only unlinked by RemoveAll, so removing it is safe
		// even when it points at the cwd — the referent must be left intact and
		// removal must not be refused (nor the referent's tree chmod-ed).
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("os.Getwd: %v", err)
		}
		link := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(cwd, link); err != nil {
			t.Fatalf("Symlink: %v", err)
		}
		if err := cleanPath(t.Context(), link); err != nil {
			t.Fatalf("removing a symlink to cwd should be safe, got: %v", err)
		}
		if _, err := os.Lstat(link); !os.IsNotExist(err) {
			t.Errorf("symlink should be removed, got err=%v", err)
		}
		if _, err := os.Stat(cwd); err != nil {
			t.Errorf("cwd (the referent) must be left intact: %v", err)
		}
	})

	t.Run("rejects an ancestor of the working directory", func(t *testing.T) {
		// A target that contains the cwd (e.g. "/work" while cwd is "/work/job")
		// must be refused — RemoveAll would recursively delete the cwd too.
		base := t.TempDir()
		job := filepath.Join(base, "job")
		if err := os.MkdirAll(job, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		t.Chdir(job) // cwd is now base/job; restored automatically after the test

		err := cleanPath(t.Context(), base)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "refusing to remove") {
			t.Errorf("error %q should contain %q", err.Error(), "refusing to remove")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		dir := t.TempDir()
		testDir := filepath.Join(dir, "cache")
		if err := os.MkdirAll(testDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err := cleanPath(ctx, testDir)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})
}

// TestPathContains covers the ancestor/equality logic behind cleanPath's
// protected-directory guard.
func TestPathContains(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "work")
	child := filepath.Join(parent, "job")
	sibling := filepath.Join(base, "other")
	for _, d := range []string{child, sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}

	parentInfo, err := os.Stat(parent)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	childInfo, err := os.Stat(child)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if !pathContains(parentInfo, parent) {
		t.Error("a directory should contain itself")
	}
	if !pathContains(parentInfo, child) {
		t.Error("parent should contain its descendant")
	}
	if pathContains(parentInfo, sibling) {
		t.Error("parent should not contain a sibling")
	}
	if pathContains(childInfo, parent) {
		t.Error("child should not contain its parent")
	}
}

func TestCleanPathWindowsDriveRoot(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}

	err := cleanPath(t.Context(), "C:\\")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "refusing to remove drive root") {
		t.Errorf("error %q should contain %q", err.Error(), "refusing to remove drive root")
	}
}

func TestCleanPathWindowsUNCShareRoot(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UNC share roots are Windows-only")
	}

	// A UNC share root cleans to the volume name with no trailing separator, so
	// the drive-root guard misses it — cleanup must still refuse it.
	err := cleanPath(t.Context(), `\\server\share`)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "refusing to remove") {
		t.Errorf("error %q should contain %q", err.Error(), "refusing to remove")
	}
}
