package clicommand

import (
	"fmt"

	"github.com/buildkite/agent/v4/agent"
)

// resolveExecutor returns the canonical executor name from the executor
// setting and the --kubernetes-exec alias. An unknown name is an error, so a
// typo can never fall back to running jobs on the host.
func resolveExecutor(executor string, kubernetesExec bool) (string, error) {
	switch executor {
	case "":
		if kubernetesExec {
			return agent.ExecutorKubernetes, nil
		}
		return agent.ExecutorExec, nil
	case agent.ExecutorExec, agent.ExecutorKubernetes:
	default:
		return "", fmt.Errorf("unknown executor %q, must be one of %v", executor, []string{agent.ExecutorExec, agent.ExecutorKubernetes})
	}
	if kubernetesExec && executor != agent.ExecutorKubernetes {
		return "", fmt.Errorf("kubernetes-exec cannot be combined with executor %q (kubernetes-exec means executor %q)", executor, agent.ExecutorKubernetes)
	}
	return executor, nil
}
