package bootstrap

import (
	"testing"

	"github.com/buildkite/agent/v3/bootstrap/shell"
	"github.com/stretchr/testify/assert"
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
		assert.Equal(t, test.expected, dirForAgentName(test.agentName))
	}
}

func TestGetValuesToRedact(t *testing.T) {
	t.Parallel()

	redactConfig := []string{
		"*_PASSWORD",
		"*_TOKEN",
	}
	environment := map[string]string{
		"BUILDKITE_PIPELINE": "unit-test",
		"DATABASE_USERNAME":  "AzureDiamond",
		"DATABASE_PASSWORD":  "hunter2",
	}

	valuesToRedact := getValuesToRedact(shell.DiscardLogger, redactConfig, environment)

	assert.Equal(t, valuesToRedact, []string{"hunter2"})
}
