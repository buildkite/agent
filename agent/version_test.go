package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVersionIsValid(t *testing.T) {
	// Making sure the version string matches `1.2.3`, `1.2.3-beta`, `1.2.3-beta.1` or `1.2-beta.1`
	assert.Regexp(t, `\A(?:\d+\.){1,2}\d+(?:-[a-zA-Z]+(?:\.\d+)?)?\z`, Version())
}
