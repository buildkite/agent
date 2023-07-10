package agent

import (
	"strings"

	"github.com/buildkite/agent/v3/env"
)

const k8sEnvVarPrefix = "BUILDKITE_K8S_"

func K8sTagsFromEnv(envn []string) (map[string]string, error) {
	envMap := make(map[string]string, len(envn))
	for _, e := range envn {
		if k, v, ok := env.Split(e); ok && strings.HasPrefix(k, k8sEnvVarPrefix) {
			envMap[envVarToTagKey(k)] = v
		}
	}
	return envMap, nil
}

func envVarToTagKey(k string) string {
	trimmed := strings.TrimPrefix(k, k8sEnvVarPrefix)
	lowered := strings.ToLower(trimmed)
	kebabed := strings.ReplaceAll(lowered, "_", "-")
	return "k8s:" + kebabed
}
