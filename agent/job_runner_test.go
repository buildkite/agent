package agent

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
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

func TestNormalizeJobEnv(t *testing.T) {
	tests := []struct {
		name      string
		goos      string
		input     map[string]string
		want      map[string]string
		wantError bool
	}{
		{"windows_plugin_alias", "windows", map[string]string{"buildkite_plugins": "[]"}, map[string]string{"BUILDKITE_PLUGINS": "[]"}, false},
		{"windows_token_alias", "windows", map[string]string{"buildkite_agent_token": "secret"}, map[string]string{"BUILDKITE_AGENT_TOKEN": "secret"}, false},
		{"windows_collision", "windows", map[string]string{"BUILDKITE_REPO": "safe", "buildkite_repo": "unsafe"}, nil, true},
		{"linux_distinct", "linux", map[string]string{"BUILDKITE_REPO": "safe", "buildkite_repo": "unsafe"}, map[string]string{"BUILDKITE_REPO": "safe", "buildkite_repo": "unsafe"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeJobEnv(tt.input, tt.goos)
			if (err != nil) != tt.wantError {
				t.Fatalf("normalizeJobEnv() error = %v, want error %v", err, tt.wantError)
			}
			if tt.wantError {
				return
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("normalizeJobEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWindowsPluginAliasReachesAllowlist(t *testing.T) {
	jobEnv := map[string]string{
		"buildkite_plugins": `[{"github.com/other/plugin#v1":{}}]`,
	}
	normalized, err := normalizeJobEnv(jobEnv, "windows")
	if err != nil {
		t.Fatal(err)
	}
	job := &api.Job{Env: normalized}
	r := &JobRunner{
		conf: JobRunnerConfig{
			Job: job,
			AgentConfiguration: AgentConfiguration{
				PluginsEnabled: true,
				AllowedPlugins: []*regexp.Regexp{regexp.MustCompile(`^github\.com/buildkite-plugins/`)},
			},
		},
		signedJobEnv: jobEnv,
	}
	if err := r.validateConfigAllowlists(job); err == nil {
		t.Fatal("case-variant plugin escaped the allowlist")
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

func TestRemoveKubernetesPlugin(t *testing.T) {
	const k8s = `{"github.com/buildkite-plugins/kubernetes-buildkite-plugin":{"podTemplate":"example"}}`
	const other = `{"github.com/buildkite-plugins/docker-buildkite-plugin#v5":{"image":"alpine"}}`

	tests := []struct {
		name, in, want string
	}{
		{"only kubernetes plugin", "[" + k8s + "]", ""},
		{"kubernetes and other plugin", "[" + k8s + "," + other + "]", "[" + other + "]"},
		{"no kubernetes plugin", "[" + other + "]", "[" + other + "]"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := removeKubernetesPlugin([]byte(test.in))
			if err != nil {
				t.Fatalf("removeKubernetesPlugin(%q) error = %v", test.in, err)
			}
			if string(got) != test.want {
				t.Errorf("removeKubernetesPlugin(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}
