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

	"github.com/buildkite/agent/v3/api"
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

	t.Run("rejects home directory", func(t *testing.T) {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("UserHomeDir: %v", err)
		}

		err = cleanPath(t.Context(), home)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "refusing to remove home directory") {
			t.Errorf("error %q should contain %q", err.Error(), "refusing to remove home directory")
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
