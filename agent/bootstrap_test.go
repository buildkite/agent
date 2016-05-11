package agent

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

var agentNameTests = []struct {
	agentName   string
	expected    string
}{
	{"My Agent", "My-Agent"},
	{":docker: My Agent", "-docker--My-Agent"},
	{"My \"Agent\"", "My--Agent-"},
}

func TestDirForAgentName(t *testing.T) {
	for _, test := range agentNameTests {
		assert.Equal(t, test.expected, dirForAgentName(test.agentName))
	}
}
