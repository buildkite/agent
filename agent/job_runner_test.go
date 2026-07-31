package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/logger"
)

func TestTruncateEnv(t *testing.T) {
	l := logger.NewBuffer()
	key := "FOO"
	env := map[string]string{key: strings.Repeat("a", 100)}
	limit := 64
	if err := truncateEnv(l, env, key, limit); err != nil {
		t.Fatalf("truncateEnv(logger, %v, %q, %d) = %v", env, key, limit, err)
	}
	if got, want := env["FOO"], "aaaaaaaaaaaaaaaaaaaaaaaaaa[value truncated 100 -> 59 bytes]"; got != want {
		t.Errorf("after truncateEnv(logger, %v, %q, %d): env[%q] = %q, want %q", env, key, limit, key, got, want)
	}
	format := "FOO=%s\000"
	if got, want := len(fmt.Sprintf(format, env["FOO"])), limit; got != want {
		t.Errorf("after truncateEnv(logger, %v, %q, %d): len(fmt.Sprintf(%q, env[%q])) = %d, want %d", env, key, limit, format, key, got, want)
	}
}

func TestValidateJobValue(t *testing.T) {
	bkTarget := "github.com/buildkite/test"
	bkTargetRE := regexp.MustCompile(`^github\.com/buildkite/.*`)
	ghTargetRE := regexp.MustCompile(`^github\.com/nope/.*`)

	tests := []struct {
		name           string
		allowedTargets []*regexp.Regexp
		pipelineTarget string
		wantErr        bool
	}{
		{
			name:           "No error. Allowed targets no configured.",
			allowedTargets: []*regexp.Regexp{},
			pipelineTarget: bkTarget,
		}, {
			name:           "No pipeline target match",
			allowedTargets: []*regexp.Regexp{ghTargetRE},
			pipelineTarget: bkTarget,
			wantErr:        true,
		}, {
			name:           "Pipeline target match",
			allowedTargets: []*regexp.Regexp{ghTargetRE, bkTargetRE},
			pipelineTarget: bkTarget,
		},
	}

	for _, tc := range tests {
		err := validateJobValue(tc.allowedTargets, tc.pipelineTarget)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateJobValue() error = %v, wantErr = %v", err, tc.wantErr)
		}
	}
}

func TestJobTimeoutFilePath(t *testing.T) {
	t.Parallel()

	got := jobTimeoutFilePath("abc123", jobContextDir(JobRunnerConfig{}))
	want := filepath.Join(os.TempDir(), "job-timeout-abc123")
	if got != want {
		t.Errorf("jobTimeoutFilePath(%q, jobContextDir({})) = %q, want %q", "abc123", got, want)
	}

	k8sDir := jobContextDir(JobRunnerConfig{KubernetesExec: true})
	if got, want := jobTimeoutFilePath("abc123", k8sDir), filepath.Join("/workspace", "job-timeout-abc123"); got != want {
		t.Errorf("jobTimeoutFilePath(%q, %q) = %q, want %q", "abc123", k8sDir, got, want)
	}
}

func TestJobContextDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		conf JobRunnerConfig
		want string
	}{
		{
			name: "default",
			conf: JobRunnerConfig{},
			want: os.TempDir(),
		},
		{
			name: "explicit_dir",
			conf: JobRunnerConfig{JobContextDir: "/var/lib/buildkite/job"},
			want: "/var/lib/buildkite/job",
		},
		{
			name: "kubernetes_default",
			conf: JobRunnerConfig{KubernetesExec: true},
			want: "/workspace",
		},
		{
			name: "kubernetes_explicit_dir",
			conf: JobRunnerConfig{
				KubernetesExec: true,
				JobContextDir:  "/buildkite-shared",
			},
			want: "/buildkite-shared",
		},
	}

	for _, tc := range tests {
		if got := jobContextDir(tc.conf); got != tc.want {
			t.Errorf("%s: jobContextDir(%+v) = %q, want %q", tc.name, tc.conf, got, tc.want)
		}
	}
}

func TestCancelReasonString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason CancelReason
		want   string
	}{
		{CancelReasonJobState, "job cancelled on Buildkite"},
		{CancelReasonAgentStopping, "agent is stopping"},
		{CancelReasonInvalidToken, "access token is invalid"},
		{CancelReasonJobTimeout, "job timed out on Buildkite"},
		{CancelReason(99), "unknown"},
	}

	for _, tc := range tests {
		if got := tc.reason.String(); got != tc.want {
			t.Errorf("CancelReason(%d).String() = %q, want %q", tc.reason, got, tc.want)
		}
	}
}
