package clicommand

import (
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/internal/dockerexec"
	"github.com/buildkite/agent/v4/internal/process"
	"github.com/google/go-cmp/cmp"
)

func TestResolveExecutor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		executor       string
		kubernetesExec bool
		want           string
		wantErr        string
	}{
		{name: "default", want: "exec"},
		{name: "exec", executor: "exec", want: "exec"},
		{name: "kubernetes", executor: "kubernetes", want: "kubernetes"},
		{name: "docker", executor: "docker", want: "docker"},
		{name: "kubernetes_exec_alias", kubernetesExec: true, want: "kubernetes"},
		{name: "kubernetes_exec_and_kubernetes", executor: "kubernetes", kubernetesExec: true, want: "kubernetes"},
		{name: "kubernetes_exec_conflicts_with_exec", executor: "exec", kubernetesExec: true, wantErr: "cannot be combined"},
		{name: "kubernetes_exec_conflicts_with_docker", executor: "docker", kubernetesExec: true, wantErr: "cannot be combined"},
		{name: "unknown_rejected_before_conflict", executor: "dokcer", kubernetesExec: true, wantErr: "unknown executor"},
		{name: "typo", executor: "dokcer", wantErr: "unknown executor"},
		{name: "case_sensitive", executor: "Exec", wantErr: "unknown executor"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveExecutor(tc.executor, tc.kubernetesExec)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("resolveExecutor(%q, %t) = (%q, %v), want an error containing %q", tc.executor, tc.kubernetesExec, got, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveExecutor(%q, %t) error = %v", tc.executor, tc.kubernetesExec, err)
			}
			if got != tc.want {
				t.Errorf("resolveExecutor(%q, %t) = %q, want %q", tc.executor, tc.kubernetesExec, got, tc.want)
			}
		})
	}
}

func TestDockerExecutorConfig(t *testing.T) {
	t.Parallel()

	got := dockerExecutorConfig(AgentStartConfig{
		ExecutorDockerImage:   "debian:stable-slim",
		ExecutorDockerMount:   []string{"/srv/cache:/cache"},
		ExecutorDockerEnv:     []string{"HTTP_PROXY"},
		ExecutorDockerNetwork: "builds",
		BuildPath:             "/var/lib/buildkite-agent/builds",
		PluginsPath:           "/var/lib/buildkite-agent/plugins",
		GitMirrorsPath:        "/var/lib/buildkite-agent/git-mirrors",
		SocketsPath:           "/var/lib/buildkite-agent/sockets",
		JobContextDir:         "/var/lib/buildkite-agent/job-context",
		HooksPath:             "/etc/buildkite-agent/hooks",
		AdditionalHooksPaths:  []string{"/opt/hooks"},
		SigningJWKSFile:       "/etc/buildkite-agent/jwks.json",
		NoPTY:                 true,
	}, process.SIGINT)

	want := dockerexec.Config{
		Image:           "debian:stable-slim",
		Mounts:          []string{"/srv/cache:/cache"},
		Env:             []string{"HTTP_PROXY"},
		Network:         "builds",
		BuildPath:       "/var/lib/buildkite-agent/builds",
		PluginsPath:     "/var/lib/buildkite-agent/plugins",
		GitMirrorsPath:  "/var/lib/buildkite-agent/git-mirrors",
		SocketsPath:     "/var/lib/buildkite-agent/sockets",
		JobContextDir:   "/var/lib/buildkite-agent/job-context",
		HooksPaths:      []string{"/etc/buildkite-agent/hooks", "/opt/hooks"},
		SigningJWKSFile: "/etc/buildkite-agent/jwks.json",
		RunInPty:        false,
		CancelSignal:    process.SIGINT,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("dockerExecutorConfig() diff (-want +got):\n%s", diff)
	}
}
