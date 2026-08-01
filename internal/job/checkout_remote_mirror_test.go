package job

import (
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/self"
	"github.com/buildkite/shellwords"
)

func TestShouldAttemptRemoteMirror(t *testing.T) {
	t.Parallel()

	commit := strings.Repeat("a", 40)
	base := ExecutorConfig{
		Commit:                    commit,
		GitRemoteMirrorURL:        "https://mirror.example.com/acme/widgets.git",
		GitRemoteMirrorRepository: "https://canonical.example.com/acme/widgets.git",
		PipelineProvider:          "github",
		PullRequest:               "false",
		Repository:                "https://canonical.example.com/acme/widgets.git",
	}
	tests := []struct {
		name             string
		mutate           func(*ExecutorConfig)
		previousAttempts int
		want             bool
	}{
		{name: "eligible", want: true},
		{name: "empty URL", mutate: func(c *ExecutorConfig) { c.GitRemoteMirrorURL = "" }},
		{name: "local URL", mutate: func(c *ExecutorConfig) { c.GitRemoteMirrorURL = "file:///tmp/mirror.git" }},
		{name: "SSH URL", mutate: func(c *ExecutorConfig) { c.GitRemoteMirrorURL = "ssh://mirror.example.com/repo.git" }},
		{name: "HEAD", mutate: func(c *ExecutorConfig) { c.Commit = "HEAD" }},
		{name: "short SHA", mutate: func(c *ExecutorConfig) { c.Commit = commit[:12] }},
		{name: "tag", mutate: func(c *ExecutorConfig) { c.Tag = "v1.0.0" }},
		{name: "pull request", mutate: func(c *ExecutorConfig) { c.PullRequest = "42" }},
		{name: "custom refspec", mutate: func(c *ExecutorConfig) { c.RefSpec = "refs/heads/main" }},
		{name: "repository changed by hook", mutate: func(c *ExecutorConfig) { c.Repository = "https://proxy.example.com/acme/widgets.git" }},
		{name: "later checkout attempt", previousAttempts: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := base
			if tc.mutate != nil {
				tc.mutate(&cfg)
			}
			e := &Executor{ExecutorConfig: cfg}
			if got := e.shouldAttemptRemoteMirror(tc.previousAttempts); got != tc.want {
				t.Errorf("shouldAttemptRemoteMirror(%d) = %t, want %t", tc.previousAttempts, got, tc.want)
			}
		})
	}
}

func TestIsSupportedRemoteMirrorURL(t *testing.T) {
	t.Parallel()

	for rawURL, want := range map[string]bool{
		"https://mirror.example.com/repo.git": true,
		"http://mirror.example.com/repo.git":  true,
		"ssh://mirror.example.com/repo.git":   false,
		"file:///tmp/repo.git":                false,
		"mirror.example.com/repo.git":         false,
		"://not-a-url":                        false,
	} {
		t.Run(rawURL, func(t *testing.T) {
			t.Parallel()
			if got := isSupportedRemoteMirrorURL(rawURL); got != want {
				t.Errorf("isSupportedRemoteMirrorURL(%q) = %t, want %t", rawURL, got, want)
			}
		})
	}
}

func TestGitCredentialHelperFlagsAreInvocationScoped(t *testing.T) {
	t.Parallel()

	helperPath := "/tmp/Buildkite Agent;safe"
	parts := gitCredentialHelperFlags(self.OverridePath(t.Context(), helperPath))
	if len(parts) != 6 {
		t.Fatalf("gitCredentialHelperFlags() returned %d parts, want 6: %q", len(parts), parts)
	}
	if parts[0] != "-c" || parts[1] != "credential.useHttpPath=true" {
		t.Errorf("gitCredentialHelperFlags() does not enable HTTP path matching: %q", parts)
	}
	if parts[2] != "-c" || parts[3] != "credential.helper=" {
		t.Errorf("gitCredentialHelperFlags() does not clear inherited helpers: %q", parts)
	}
	if parts[4] != "-c" || !strings.HasPrefix(parts[5], "credential.helper=") {
		t.Errorf("gitCredentialHelperFlags() does not install the agent helper: %q", parts)
	}
	helperCommand := strings.TrimPrefix(parts[5], "credential.helper=")
	commandParts, err := shellwords.Split(helperCommand)
	if err != nil {
		t.Fatalf("credential helper command %q is not valid shell syntax: %v", helperCommand, err)
	}
	if len(commandParts) != 2 || commandParts[0] != helperPath || commandParts[1] != "git-credentials-helper" {
		t.Errorf("credential helper command split into %q, want [%q %q]", commandParts, helperPath, "git-credentials-helper")
	}
}
