package agent_test

import (
	"testing"

	"github.com/buildkite/agent/v3/agent"
	"github.com/google/go-cmp/cmp"
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
			if err != nil {
				t.Fatalf("agent.K8sTagsFromEnv(test.env) error = %v, want nil", err)
			}
			if diff := cmp.Diff(actualTags, test.expectedTags); diff != "" {
				t.Errorf("agent.K8sTagsFromEnv(test.env) diff (-got +want):\n%s", diff)
			}
		})
	}
}
