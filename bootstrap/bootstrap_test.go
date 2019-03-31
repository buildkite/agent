package bootstrap

import (
	"testing"

	"gotest.tools/assert"
	is "gotest.tools/assert/cmp"
)

var agentNameTests = []struct {
	agentName string
	expected  string
}{
	{"My Agent", "My-Agent"},
	{":docker: My Agent", "-docker--My-Agent"},
	{"My \"Agent\"", "My--Agent-"},
}

func TestDirForAgentName(t *testing.T) {
	t.Parallel()

	for _, test := range agentNameTests {
		assert.Check(t, is.Equal(test.expected, dirForAgentName(test.agentName)))
	}
}
