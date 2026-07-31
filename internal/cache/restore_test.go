package cache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/api"
)

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

// A scoped entry lives at a distinct address, so invalidating it requires the
// matched entry's scopes. invalidateStaleEntry must echo the scopes from the
// retrieve response into the expire request, or a scoped registry's stale entry
// can never be busted.
func TestInvalidateStaleEntryEchoesScopes(t *testing.T) {
	ctx := t.Context()

	cacheClient, _, _ := setupTestCache(t, "local_file")
	mockClient := cacheClient.api.(*mockAPIClient)

	scopes := map[string]string{"branch": "main", "pipeline": "app"}
	retrieveResp := api.CacheEntryRetrieveResp{
		TargetPaths: []string{"node_modules"},
		CacheKey:    []api.CacheKeyPart{{Value: "v1-key"}},
		Scopes:      scopes,
	}

	if ok := cacheClient.invalidateStaleEntry(ctx, retrieveResp); !ok {
		t.Fatal("invalidateStaleEntry returned false, want true")
	}

	if len(mockClient.expireCalls) != 1 {
		t.Fatalf("expire calls = %d, want 1", len(mockClient.expireCalls))
	}
	if got := mockClient.expireCalls[0].Scopes; !reflect.DeepEqual(got, scopes) {
		t.Errorf("expire request Scopes = %v, want %v", got, scopes)
	}
}
