package glob

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRoot(t *testing.T) {
	expected, _ := os.Getwd()
	assert.Equal(t, expected, Root("tmp/**/*.png"))

	assert.Equal(t, "/", Root("/tmp/artifacts/*.png"))
}
