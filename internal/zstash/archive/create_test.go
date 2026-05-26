package archive

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/buildkite/agent/v3/internal/zstash/internal/trace"
	"github.com/stretchr/testify/require"
)

func TestBuildArchive(t *testing.T) {
	assert := require.New(t)

	_, err := trace.NewProvider(context.Background(), "noop", "test", "0.0.1")
	assert.NoError(err)

	home, err := os.Getwd()
	assert.NoError(err)

	t.Setenv("HOME", home)

	archiveInfo, err := BuildArchive(context.Background(), []string{"testdata"}, "test")
	assert.NoError(err)
	assert.Equal("3f194172652432099ffc528f81ea6ce1687780287cba1d1a9587f5c26c72aeac", archiveInfo.Sha256sum)
	assert.Equal(int64(1228), archiveInfo.Size)

	homeDir, err := os.UserHomeDir()
	assert.NoError(err)
	assert.Equal(home, homeDir)
}

func TestBuildAndExtractArchive_MultipleHomeDirPaths(t *testing.T) {
	assert := require.New(t)

	_, err := trace.NewProvider(context.Background(), "noop", "test", "0.0.1")
	assert.NoError(err)

	home := t.TempDir()
	t.Setenv("HOME", home)

	goBuildDir := filepath.Join(home, ".go-build")
	err = os.MkdirAll(goBuildDir, 0o755)
	assert.NoError(err)

	err = os.WriteFile(filepath.Join(goBuildDir, "cache.txt"), []byte("build cache data"), 0o600)
	assert.NoError(err)

	goModDir := filepath.Join(home, "go", "pkg", "mod")
	err = os.MkdirAll(goModDir, 0o755)
	assert.NoError(err)

	err = os.WriteFile(filepath.Join(goModDir, "module.txt"), []byte("module cache data"), 0o600)
	assert.NoError(err)

	paths := []string{
		"~/.go-build",
		"~/go/pkg/mod",
	}

	archiveInfo, err := BuildArchive(context.Background(), paths, "go-cache")
	assert.NoError(err)
	assert.NotEmpty(archiveInfo.ArchivePath)
	assert.Greater(archiveInfo.Size, int64(0))
	assert.NotEmpty(archiveInfo.Sha256sum)

	defer os.Remove(archiveInfo.ArchivePath)

	err = os.RemoveAll(goBuildDir)
	assert.NoError(err)
	err = os.RemoveAll(filepath.Join(home, "go"))
	assert.NoError(err)

	_, err = os.Stat(goBuildDir)
	assert.True(os.IsNotExist(err))
	_, err = os.Stat(goModDir)
	assert.True(os.IsNotExist(err))

	zipFile, err := os.Open(archiveInfo.ArchivePath)
	assert.NoError(err)
	defer zipFile.Close()

	entries, err := ListArchive(context.Background(), zipFile, archiveInfo.Size)
	assert.NoError(err)
	assert.Contains(entries, ".go-build/cache.txt")
	assert.Contains(entries, "go/pkg/mod/module.txt")

	_, err = zipFile.Seek(0, 0)
	assert.NoError(err)

	extractInfo, err := ExtractFiles(context.Background(), zipFile, archiveInfo.Size, paths)
	assert.NoError(err)
	assert.Greater(extractInfo.WrittenEntries, int64(0))

	cacheContent, err := os.ReadFile(filepath.Join(goBuildDir, "cache.txt"))
	assert.NoError(err)
	assert.Equal("build cache data", string(cacheContent))

	moduleContent, err := os.ReadFile(filepath.Join(goModDir, "module.txt"))
	assert.NoError(err)
	assert.Equal("module cache data", string(moduleContent))
}

func TestBuildArchive_MissingPathOnFilesystem(t *testing.T) {
	assert := require.New(t)

	_, err := trace.NewProvider(context.Background(), "noop", "test", "0.0.1")
	assert.NoError(err)

	home := t.TempDir()
	t.Setenv("HOME", home)

	goBuildDir := filepath.Join(home, ".go-build")
	err = os.MkdirAll(goBuildDir, 0o755)
	assert.NoError(err)

	err = os.WriteFile(filepath.Join(goBuildDir, "cache.txt"), []byte("build cache data"), 0o600)
	assert.NoError(err)

	paths := []string{
		"~/.go-build",
		"~/go/pkg/mod",
	}

	archiveInfo, err := BuildArchive(context.Background(), paths, "go-cache")
	assert.NoError(err)
	defer os.Remove(archiveInfo.ArchivePath)

	zipFile, err := os.Open(archiveInfo.ArchivePath)
	assert.NoError(err)
	defer zipFile.Close()

	entries, err := ListArchive(context.Background(), zipFile, archiveInfo.Size)
	assert.NoError(err)
	assert.Contains(entries, ".go-build/cache.txt")

	for _, entry := range entries {
		assert.NotContains(entry, "go/pkg/mod", "archive should not contain the missing path")
	}
}

func TestExtractArchive_MissingPathInArchive(t *testing.T) {
	assert := require.New(t)

	_, err := trace.NewProvider(context.Background(), "noop", "test", "0.0.1")
	assert.NoError(err)

	home := t.TempDir()
	t.Setenv("HOME", home)

	goBuildDir := filepath.Join(home, ".go-build")
	err = os.MkdirAll(goBuildDir, 0o755)
	assert.NoError(err)

	err = os.WriteFile(filepath.Join(goBuildDir, "cache.txt"), []byte("build cache data"), 0o600)
	assert.NoError(err)

	archiveInfo, err := BuildArchive(context.Background(), []string{"~/.go-build"}, "go-cache")
	assert.NoError(err)
	defer os.Remove(archiveInfo.ArchivePath)

	err = os.RemoveAll(goBuildDir)
	assert.NoError(err)

	zipFile, err := os.Open(archiveInfo.ArchivePath)
	assert.NoError(err)
	defer zipFile.Close()

	entries, err := ListArchive(context.Background(), zipFile, archiveInfo.Size)
	assert.NoError(err)
	assert.Contains(entries, ".go-build/cache.txt")
	assert.NotContains(entries, "go/pkg/mod/")

	_, err = zipFile.Seek(0, 0)
	assert.NoError(err)

	pathsWithMissing := []string{
		"~/.go-build",
		"~/go/pkg/mod",
	}

	extractInfo, err := ExtractFiles(context.Background(), zipFile, archiveInfo.Size, pathsWithMissing)
	assert.NoError(err)
	assert.Greater(extractInfo.WrittenEntries, int64(0))

	cacheContent, err := os.ReadFile(filepath.Join(goBuildDir, "cache.txt"))
	assert.NoError(err)
	assert.Equal("build cache data", string(cacheContent))

	goModDir := filepath.Join(home, "go", "pkg", "mod")
	_, err = os.Stat(goModDir)
	assert.True(os.IsNotExist(err), "go/pkg/mod should not exist since it wasn't in the archive")
}
