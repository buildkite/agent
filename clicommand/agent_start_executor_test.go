package clicommand

import (
	"strings"
	"testing"
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
		{name: "kubernetes_exec_alias", kubernetesExec: true, want: "kubernetes"},
		{name: "kubernetes_exec_and_kubernetes", executor: "kubernetes", kubernetesExec: true, want: "kubernetes"},
		{name: "kubernetes_exec_conflicts_with_exec", executor: "exec", kubernetesExec: true, wantErr: "cannot be combined"},
		{name: "unknown_rejected_before_conflict", executor: "docker", kubernetesExec: true, wantErr: "unknown executor"},
		{name: "docker_unknown", executor: "docker", wantErr: "unknown executor"},
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
