package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/assert"
)

func findArtifact(artifacts []*api.Artifact, search string) *api.Artifact {
	for _, a := range artifacts {
		if filepath.Base(a.Path) == search {
			return a
		}
	}

	return nil
}

func TestCollect(t *testing.T) {
	t.Parallel()

	wd, _ := os.Getwd()
	root := filepath.Join(wd, "..")
	os.Chdir(root)
	defer os.Chdir(wd)

	volumeName := filepath.VolumeName(root)
	rootWithoutVolume := strings.TrimPrefix(root, volumeName)

	uploader := NewArtifactUploader(logger.Discard, nil, ArtifactUploaderConfig{
		Paths: fmt.Sprintf("%s;%s",
			filepath.Join("test", "fixtures", "artifacts", "**/*.jpg"),
			filepath.Join(root, "test", "fixtures", "artifacts", "**/*.gif"),
		),
	})

	artifacts, err := uploader.Collect()
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, len(artifacts), 4)

	var testCases = []struct {
		Name         string
		Path         string
		AbsolutePath string
		GlobPath     string
		FileSize     int
		Sha1Sum      string
	}{
		{
			Name:         "Mr Freeze.jpg",
			Path:         strings.Join([]string{"test", "fixtures", "artifacts", "Mr Freeze.jpg"}, "/"),
			AbsolutePath: filepath.Join(root, "test", "fixtures", "artifacts", "Mr Freeze.jpg"),
			GlobPath:     filepath.Join("test", "fixtures", "artifacts", "**", "*.jpg"),
			FileSize:     362371,
			Sha1Sum:      "f5bc7bc9f5f9c3e543dde0eb44876c6f9acbfb6b",
		},
		{
			Name:         "Commando.jpg",
			Path:         strings.Join([]string{"test", "fixtures", "artifacts", "folder", "Commando.jpg"}, "/"),
			AbsolutePath: filepath.Join(root, "test", "fixtures", "artifacts", "folder", "Commando.jpg"),
			GlobPath:     filepath.Join("test", "fixtures", "artifacts", "**", "*.jpg"),
			FileSize:     113000,
			Sha1Sum:      "811d7cb0317582e22ebfeb929d601cdabea4b3c0",
		},
		{
			Name:         "The Terminator.jpg",
			Path:         strings.Join([]string{"test", "fixtures", "artifacts", "this is a folder with a space", "The Terminator.jpg"}, "/"),
			AbsolutePath: filepath.Join(root, "test", "fixtures", "artifacts", "this is a folder with a space", "The Terminator.jpg"),
			GlobPath:     filepath.Join("test", "fixtures", "artifacts", "**", "*.jpg"),
			FileSize:     47301,
			Sha1Sum:      "ed76566ede9cb6edc975fcadca429665aad8785a",
		},
		{
			Name:         "Smile.gif",
			Path:         strings.Join([]string{rootWithoutVolume[1:], "test", "fixtures", "artifacts", "gifs", "Smile.gif"}, "/"),
			AbsolutePath: filepath.Join(root, "test", "fixtures", "artifacts", "gifs", "Smile.gif"),
			GlobPath:     filepath.Join(root, "test", "fixtures", "artifacts", "**", "*.gif"),
			FileSize:     2038453,
			Sha1Sum:      "bd4caf2e01e59777744ac1d52deafa01c2cb9bfd",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			a := findArtifact(artifacts, tc.Name)
			if a == nil {
				t.Fatalf("Failed to find artifact %q", tc.Name)
			}

			assert.Equal(t, tc.Path, a.Path)
			assert.Equal(t, tc.AbsolutePath, a.AbsolutePath)
			assert.Equal(t, tc.GlobPath, a.GlobPath)
			assert.Equal(t, tc.FileSize, int(a.FileSize))
			assert.Equal(t, tc.Sha1Sum, a.Sha1Sum)
		})
	}
}

func TestCollectThatDoesntMatchAnyFiles(t *testing.T) {
	wd, _ := os.Getwd()
	root := filepath.Join(wd, "..")
	os.Chdir(root)
	defer os.Chdir(wd)

	uploader := NewArtifactUploader(logger.Discard, nil, ArtifactUploaderConfig{
		Paths: strings.Join([]string{
			filepath.Join("log", "*"),
			filepath.Join("tmp", "capybara", "**", "*"),
			filepath.Join("mkmf.log"),
			filepath.Join("log", "mkmf.log"),
		}, ";"),
	})

	artifacts, err := uploader.Collect()
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, len(artifacts), 0)
}

func TestCollectWithSomeGlobsThatDontMatchAnything(t *testing.T) {
	wd, _ := os.Getwd()
	root := filepath.Join(wd, "..")
	os.Chdir(root)
	defer os.Chdir(wd)

	uploader := NewArtifactUploader(logger.Discard, nil, ArtifactUploaderConfig{
		Paths: strings.Join([]string{
			filepath.Join("dontmatchanything", "*"),
			filepath.Join("dontmatchanything.zip"),
			filepath.Join("test", "fixtures", "artifacts", "**", "*.jpg"),
		}, ";"),
	})

	artifacts, err := uploader.Collect()
	if err != nil {
		t.Fatal(err)
	}

	if len(artifacts) != 3 {
		t.Fatalf("Expected to match 3 artifacts, found %d", len(artifacts))
	}
}
