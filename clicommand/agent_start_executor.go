package clicommand

import (
	"fmt"

	"github.com/buildkite/agent/v4/agent"
	"github.com/buildkite/agent/v4/internal/dockerexec"
	"github.com/buildkite/agent/v4/internal/process"
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
	case agent.ExecutorExec, agent.ExecutorKubernetes, agent.ExecutorDocker:
	default:
		return "", fmt.Errorf("unknown executor %q, must be one of %v", executor, []string{agent.ExecutorExec, agent.ExecutorDocker, agent.ExecutorKubernetes})
	}
	if kubernetesExec && executor != agent.ExecutorKubernetes {
		return "", fmt.Errorf("kubernetes-exec cannot be combined with executor %q (kubernetes-exec means executor %q)", executor, agent.ExecutorKubernetes)
	}
	return executor, nil
}

// dockerExecutorConfig returns the docker executor's configuration from the
// agent's.
func dockerExecutorConfig(cfg AgentStartConfig, cancelSignal process.Signal) dockerexec.Config {
	var hooksPaths []string
	if cfg.HooksPath != "" {
		hooksPaths = append(hooksPaths, cfg.HooksPath)
	}
	hooksPaths = append(hooksPaths, cfg.AdditionalHooksPaths...)
	return dockerexec.Config{
		Image:           cfg.ExecutorDockerImage,
		Mounts:          cfg.ExecutorDockerMount,
		Env:             cfg.ExecutorDockerEnv,
		Network:         cfg.ExecutorDockerNetwork,
		BuildPath:       cfg.BuildPath,
		PluginsPath:     cfg.PluginsPath,
		GitMirrorsPath:  cfg.GitMirrorsPath,
		SocketsPath:     cfg.SocketsPath,
		JobContextDir:   cfg.JobContextDir,
		HooksPaths:      hooksPaths,
		SigningJWKSFile: cfg.SigningJWKSFile,
		RunInPty:        !cfg.NoPTY,
		CancelSignal:    cancelSignal,
	}
}
