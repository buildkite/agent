package agent_test

import (
	"testing"

	"github.com/buildkite/agent/v3/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestK8sTags(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		env          []string
		expectedTags map[string]string
	}{
		{
			name:         "empty",
			env:          []string{},
			expectedTags: map[string]string{},
		},
		{
			name: "node_name",
			env:  []string{"BUILDKITE_K8S_NODE=my-node"},
			expectedTags: map[string]string{
				"k8s:node": "my-node",
			},
		},
		{
			name: "full",
			env: []string{
				"BUILDKITE_K8S_NODE=my-node",
				"BUILDKITE_K8S_NAMESPACE=buildkite",
				"BUILDKITE_K8S_SERVICE_ACCOUNT=raas",
			},
			expectedTags: map[string]string{
				"k8s:node":            "my-node",
				"k8s:namespace":       "buildkite",
				"k8s:service-account": "raas",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			actualTags, err := agent.K8sTagsFromEnv(test.env)
			require.NoError(t, err)
			assert.Equal(t, test.expectedTags, actualTags)
		})
	}
}
