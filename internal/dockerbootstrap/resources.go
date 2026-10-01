package dockerbootstrap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
)

func resourceLabels(env map[string]string) []string {
	args := []string{"--label", "com.buildkite.bootstrap=docker", "--label", "com.buildkite.agent=true"}
	for _, label := range []struct{ key, variable string }{
		{"com.buildkite.job-id", "BUILDKITE_JOB_ID"},
		{"com.buildkite.agent-id", "BUILDKITE_AGENT_ID"},
		{"com.buildkite.agent-name", "BUILDKITE_AGENT_NAME"},
	} {
		if value := env[label.variable]; value != "" {
			args = append(args, "--label", label.key+"="+value)
		}
	}
	return args
}

func (r Runner) removeResource(ctx context.Context, kind, name string) error {
	remove := []string{"rm", "--force", name}
	list := []string{"ps", "--all", "--quiet", "--filter", "name=^/" + name + "$"}
	if kind == "network" {
		remove = []string{"network", "rm", name}
		list = []string{"network", "ls", "--quiet", "--filter", "name=^" + name + "$"}
	}
	status, removeErr := r.Client.Run(ctx, remove, nil, io.Discard, io.Discard)
	if removeErr == nil && status == 0 {
		return nil
	}
	// A failed remove can mean either an absent resource or daemon loss.
	var found bytes.Buffer
	status, listErr := r.Client.Run(ctx, list, nil, &found, io.Discard)
	if listErr == nil && status == 0 && strings.TrimSpace(found.String()) == "" {
		return nil
	}
	_, _ = fmt.Fprintf(r.Stderr, "Docker bootstrap cleanup failed; %s may remain: %s: %v\n", kind, name, removeErr)
	if listErr != nil {
		_, _ = fmt.Fprintf(r.Stderr, "Could not verify %s removal: %v\n", kind, listErr)
	}
	return fmt.Errorf("docker %s cleanup failed: %w", kind, removeErr)
}
