package agent

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/buildkite/agent/api"
	"github.com/stretchr/testify/assert"
)

func findArtifact(artifacts []*api.Artifact, search string) *api.Artifact {
	for _, a := range artifacts {
		if path.Base(a.Path) == search {
			return a
		}
	}

	return nil
}

func TestCollect(t *testing.T) {
	wd, _ := os.Getwd()
	root := filepath.Join(wd, "..")
	os.Chdir(root)

	paths := fmt.Sprintf("%s;%s", filepath.Join("test", "fixtures", "artifacts", "**/*.jpg"), filepath.Join(root, "test", "fixtures", "artifacts", "**/*.gif"))
	uploader := ArtifactUploader{Paths: paths}

	artifacts, err := uploader.Collect()

	assert.Nil(t, err)
	assert.Equal(t, len(artifacts), 4)

	var a *api.Artifact

	a = findArtifact(artifacts, "Mr Freeze.jpg")
	assert.NotNil(t, a)
	assert.Equal(t, a.Path, "test/fixtures/artifacts/Mr Freeze.jpg")
	assert.Equal(t, a.AbsolutePath, filepath.Join(root, "test/fixtures/artifacts/Mr Freeze.jpg"))
	assert.Equal(t, a.GlobPath, "test/fixtures/artifacts/**/*.jpg")
	assert.Equal(t, int(a.FileSize), 362371)
	assert.Equal(t, a.Sha1Sum, "f5bc7bc9f5f9c3e543dde0eb44876c6f9acbfb6b")

	a = findArtifact(artifacts, "Commando.jpg")
	assert.NotNil(t, a)
	assert.Equal(t, a.Path, "test/fixtures/artifacts/folder/Commando.jpg")
	assert.Equal(t, a.AbsolutePath, filepath.Join(root, "test/fixtures/artifacts/folder/Commando.jpg"))
	assert.Equal(t, a.GlobPath, "test/fixtures/artifacts/**/*.jpg")
	assert.Equal(t, int(a.FileSize), 113000)
	assert.Equal(t, a.Sha1Sum, "811d7cb0317582e22ebfeb929d601cdabea4b3c0")

	a = findArtifact(artifacts, "The Terminator.jpg")
	assert.NotNil(t, a)
	assert.Equal(t, a.Path, "test/fixtures/artifacts/this is a folder with a space/The Terminator.jpg")
	assert.Equal(t, a.AbsolutePath, filepath.Join(root, "test/fixtures/artifacts/this is a folder with a space/The Terminator.jpg"))
	assert.Equal(t, a.GlobPath, "test/fixtures/artifacts/**/*.jpg")
	assert.Equal(t, int(a.FileSize), 47301)
	assert.Equal(t, a.Sha1Sum, "ed76566ede9cb6edc975fcadca429665aad8785a")

	// Need to trim the first charcater because it's path doesn't contain
	// the root, which in this case is /
	a = findArtifact(artifacts, "Smile.gif")
	assert.NotNil(t, a)
	gifPath := filepath.Join(root, "test/fixtures/artifacts/gifs/Smile.gif")
	assert.Equal(t, a.Path, gifPath[1:])
	assert.Equal(t, a.AbsolutePath, filepath.Join(root, "test/fixtures/artifacts/gifs/Smile.gif"))
	assert.Equal(t, a.GlobPath, filepath.Join(root, "test/fixtures/artifacts/**/*.gif"))
	assert.Equal(t, int(a.FileSize), 2038453)
	assert.Equal(t, a.Sha1Sum, "bd4caf2e01e59777744ac1d52deafa01c2cb9bfd")
}
