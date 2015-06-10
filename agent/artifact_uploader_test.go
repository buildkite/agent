package agent

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWorkingDirectory(t *testing.T) {
	uploader := ArtifactUploader{}

	expected, _ := os.Getwd()
	assert.Equal(t, expected, uploader.WorkingDirectory("tmp/**/*.png"))

	assert.Equal(t, "/", uploader.WorkingDirectory("/tmp/artifacts/*.png"))
}
