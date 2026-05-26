package zstash

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanPath(t *testing.T) {
	t.Run("removes directory and contents", func(t *testing.T) {
		dir := t.TempDir()
		testDir := filepath.Join(dir, "cache")
		require.NoError(t, os.MkdirAll(filepath.Join(testDir, "subdir"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "file.txt"), []byte("test"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(testDir, "subdir", "nested.txt"), []byte("nested"), 0o600))

		err := cleanPath(context.Background(), testDir)
		require.NoError(t, err)

		_, err = os.Stat(testDir)
		assert.True(t, os.IsNotExist(err), "directory should be removed")
	})

	t.Run("handles read-only directories (like go module cache)", func(t *testing.T) {
		dir := t.TempDir()
		testDir := filepath.Join(dir, "modcache")
		subdir := filepath.Join(testDir, "pkg")
		require.NoError(t, os.MkdirAll(subdir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(subdir, "mod.go"), []byte("package mod"), 0o400))
		require.NoError(t, os.Chmod(subdir, 0o555))
		require.NoError(t, os.Chmod(testDir, 0o555))

		err := cleanPath(context.Background(), testDir)
		require.NoError(t, err)

		_, err = os.Stat(testDir)
		assert.True(t, os.IsNotExist(err), "directory should be removed")
	})

	t.Run("succeeds on non-existent path", func(t *testing.T) {
		err := cleanPath(context.Background(), "/nonexistent/path/that/does/not/exist")
		require.NoError(t, err)
	})

	t.Run("rejects empty path", func(t *testing.T) {
		err := cleanPath(context.Background(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty directory path")
	})

	t.Run("rejects root path", func(t *testing.T) {
		err := cleanPath(context.Background(), "/")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "refusing to remove")
	})

	t.Run("rejects current directory", func(t *testing.T) {
		err := cleanPath(context.Background(), ".")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "refusing to remove")
	})

	t.Run("rejects home directory", func(t *testing.T) {
		home, err := os.UserHomeDir()
		require.NoError(t, err)

		err = cleanPath(context.Background(), home)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "refusing to remove home directory")
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		dir := t.TempDir()
		testDir := filepath.Join(dir, "cache")
		require.NoError(t, os.MkdirAll(testDir, 0o755))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := cleanPath(ctx, testDir)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestCleanPathWindowsDriveRoot(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}

	err := cleanPath(context.Background(), "C:\\")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to remove drive root")
}
