package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCollect(t *testing.T) {
	wd, _ := os.Getwd()
	root := filepath.Join(wd, "..")
	os.Chdir(root)

	paths := fmt.Sprintf("%s;%s", filepath.Join("test", "fixtures", "artifacts", "**/*.jpg"), filepath.Join(root, "test", "fixtures", "artifacts", "**/*.gif"))
	uploader := ArtifactUploader{Paths: paths}

	artifacts, err := uploader.Collect()

	assert.Nil(t, err)
	assert.Equal(t, len(artifacts), 4)

	assert.Equal(t, artifacts[0].Path, "test/fixtures/artifacts/Mr Freeze.jpg")
	assert.Equal(t, artifacts[0].AbsolutePath, filepath.Join(root, "test/fixtures/artifacts/Mr Freeze.jpg"))
	assert.Equal(t, artifacts[0].GlobPath, "test/fixtures/artifacts/**/*.jpg")
	assert.Equal(t, int(artifacts[0].FileSize), 362371)
	assert.Equal(t, artifacts[0].Sha1Sum, "f5bc7bc9f5f9c3e543dde0eb44876c6f9acbfb6b")

	assert.Equal(t, artifacts[1].Path, "test/fixtures/artifacts/folder/Commando.jpg")
	assert.Equal(t, artifacts[1].AbsolutePath, filepath.Join(root, "test/fixtures/artifacts/folder/Commando.jpg"))
	assert.Equal(t, artifacts[1].GlobPath, "test/fixtures/artifacts/**/*.jpg")
	assert.Equal(t, int(artifacts[1].FileSize), 113000)
	assert.Equal(t, artifacts[1].Sha1Sum, "811d7cb0317582e22ebfeb929d601cdabea4b3c0")

	assert.Equal(t, artifacts[2].Path, "test/fixtures/artifacts/this is a folder with a space/The Terminator.jpg")
	assert.Equal(t, artifacts[2].AbsolutePath, filepath.Join(root, "test/fixtures/artifacts/this is a folder with a space/The Terminator.jpg"))
	assert.Equal(t, artifacts[2].GlobPath, "test/fixtures/artifacts/**/*.jpg")
	assert.Equal(t, int(artifacts[2].FileSize), 47301)
	assert.Equal(t, artifacts[2].Sha1Sum, "ed76566ede9cb6edc975fcadca429665aad8785a")

	// Need to trim the first charcater because it's path doesn't contain
	// the root, which in this case is /
	gifPath := filepath.Join(root, "test/fixtures/artifacts/gifs/Smile.gif")
	assert.Equal(t, artifacts[3].Path, gifPath[1:len(gifPath)])
	assert.Equal(t, artifacts[3].AbsolutePath, filepath.Join(root, "test/fixtures/artifacts/gifs/Smile.gif"))
	assert.Equal(t, artifacts[3].GlobPath, filepath.Join(root, "test/fixtures/artifacts/**/*.gif"))
	assert.Equal(t, int(artifacts[3].FileSize), 2038453)
	assert.Equal(t, artifacts[3].Sha1Sum, "bd4caf2e01e59777744ac1d52deafa01c2cb9bfd")
}
