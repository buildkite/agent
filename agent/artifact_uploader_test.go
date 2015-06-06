package agent

import (
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

func TestWorkingDirectory(t *testing.T) {
	uploader := ArtifactUploader{}

	expected, _ := os.Getwd()
	assert.Equal(t, expected, uploader.WorkingDirectory("tmp/**/*.png"))

	assert.Equal(t, "/", uploader.WorkingDirectory("/tmp/artifacts/*.png"))
}
