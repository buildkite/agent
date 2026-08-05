package cache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

	t.Run("rejects top-level absolute paths", func(t *testing.T) {
		paths := []string{"/etc", "/usr", "/opt"}
		if runtime.GOOS == "windows" {
			paths = []string{`C:\Windows`, `D:\data`}
		}
		for _, path := range paths {
			err := cleanPath(t.Context(), path)
			if err == nil {
				t.Fatalf("cleanPath(%q): expected error, got nil", path)
			}
			if !strings.Contains(err.Error(), "refusing to remove top-level path") {
				t.Errorf("error %q should contain %q", err.Error(), "refusing to remove top-level path")
			}
		}
	})

	t.Run("rejects working directory and its ancestors", func(t *testing.T) {
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("Getwd: %v", err)
		}
		for _, path := range []string{"..", filepath.Join("..", ".."), cwd, filepath.Dir(cwd)} {
			_, err := validateCleanPath(path)
			if err == nil {
				t.Fatalf("validateCleanPath(%q): expected error, got nil", path)
			}
			if !strings.Contains(err.Error(), "refusing to remove") {
				t.Errorf("error %q should contain %q", err.Error(), "refusing to remove")
			}
		}
	})

	t.Run("accepts sibling paths (only ancestors are refused)", func(t *testing.T) {
		if _, err := validateCleanPath(filepath.Join("..", "sibling-cache")); err != nil {
			t.Fatalf("validateCleanPath: %v", err)
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

func TestValidateTargetPaths(t *testing.T) {
	t.Run("fails when any target path is unsafe to clean", func(t *testing.T) {
		err := validateTargetPaths([]string{filepath.Join("some", "dir"), "."})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "refusing to remove") {
			t.Errorf("error %q should contain %q", err.Error(), "refusing to remove")
		}
	})

	t.Run("fails for top-level absolute paths", func(t *testing.T) {
		path := "/etc"
		if runtime.GOOS == "windows" {
			path = `C:\Windows`
		}
		err := validateTargetPaths([]string{path})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("fails for absolute paths outside the home directory", func(t *testing.T) {
		err := validateTargetPaths([]string{filepath.Join(t.TempDir(), "opt", "cache")})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "not supported") {
			t.Errorf("error %q should contain %q", err.Error(), "not supported")
		}
	})

	t.Run("fails for parent-relative paths", func(t *testing.T) {
		err := validateTargetPaths([]string{filepath.Join("..", "outside")})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "escapes the working directory") {
			t.Errorf("error %q should contain %q", err.Error(), "escapes the working directory")
		}
	})

	t.Run("accepts safe paths", func(t *testing.T) {
		paths := []string{filepath.Join("some", "dir"), "~/cache"}
		if err := validateTargetPaths(paths); err != nil {
			t.Fatalf("validateTargetPaths(%v): %v", paths, err)
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
