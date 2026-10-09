package dockerexec

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/moby/moby/api/types/mount"
)

// validConfig returns a Config that passes validation.
func validConfig() Config {
	return Config{
		Image:         "debian:stable-slim",
		BuildPath:     "/var/lib/buildkite/builds",
		PluginsPath:   "/var/lib/buildkite/plugins",
		SocketsPath:   "/var/lib/buildkite/sockets",
		JobContextDir: "/var/lib/buildkite/job-context",
		HooksPaths:    []string{"/etc/buildkite-agent/hooks"},
		// Optional paths, set so they are covered as reserved targets.
		GitMirrorsPath:  "/var/lib/buildkite/git-mirrors",
		SigningJWKSFile: "/etc/buildkite-agent/jwks.json",
	}
}

func noEnv(string) (string, bool) { return "", false }

func TestConfigValidate_Mounts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		spec    string
		want    mount.Mount
		wantErr string
	}{
		{spec: "/srv/cache:/cache", want: mount.Mount{Type: mount.TypeBind, Source: "/srv/cache", Target: "/cache"}},
		{spec: "/srv/cache:/cache:ro", want: mount.Mount{Type: mount.TypeBind, Source: "/srv/cache", Target: "/cache", ReadOnly: true}},
		{spec: "/srv/cache:/cache:rw", want: mount.Mount{Type: mount.TypeBind, Source: "/srv/cache", Target: "/cache"}},
		{spec: "/home/agent/.ssh:/tmp/buildkite-home/.ssh:ro", want: mount.Mount{Type: mount.TypeBind, Source: "/home/agent/.ssh", Target: "/tmp/buildkite-home/.ssh", ReadOnly: true}},
		{spec: "/srv/cache:/var/lib/buildkite/builds2", want: mount.Mount{Type: mount.TypeBind, Source: "/srv/cache", Target: "/var/lib/buildkite/builds2"}},
		{spec: "/srv/cache:/var/lib/buildkite/builds-cache", want: mount.Mount{Type: mount.TypeBind, Source: "/srv/cache", Target: "/var/lib/buildkite/builds-cache"}},
		{spec: "/srv/cache", wantErr: "want src:dst"},
		{spec: "/a:/b:ro:extra", wantErr: "may not contain ':'"},
		{spec: "/srv/cache:/cache:rx", wantErr: "mode must be ro or rw"},
		{spec: "/srv/cache:/cache:", wantErr: "mode must be ro or rw"},
		{spec: "/srv/cache::ro", wantErr: "absolute"},
		{spec: "cache:/cache", wantErr: "absolute"},
		{spec: "/srv/cache:cache", wantErr: "absolute"},
		{spec: "/srv/x:/tmp/buildkite-home", wantErr: "HOME"},
		{spec: "/srv/x:/tmp/buildkite-home/", wantErr: "HOME"},
		{spec: "/srv/x:/tmp", wantErr: "HOME"},
		{spec: "/srv/x:/", wantErr: "HOME"},
		{spec: "/srv/x:/var/lib/buildkite/builds", wantErr: "overlaps /var/lib/buildkite/builds"},
		{spec: "/srv/x:/var/lib/buildkite/builds/sub/", wantErr: "overlaps /var/lib/buildkite/builds"},
		{spec: "/srv/x:/cache/../var/lib/buildkite/builds", wantErr: "overlaps /var/lib/buildkite/builds"},
		{spec: "/srv/x:/var/lib", wantErr: "overlaps /var/lib/buildkite/builds"},
		{spec: "/srv/x:/var/lib/buildkite/plugins/foo", wantErr: "overlaps /var/lib/buildkite/plugins"},
		{spec: "/srv/x:/var/lib/buildkite/job-context", wantErr: "overlaps /var/lib/buildkite/job-context"},
		{spec: "/srv/x:/var/lib/buildkite/sockets", wantErr: "overlaps /var/lib/buildkite/sockets"},
		{spec: "/srv/x:/var/lib/buildkite/git-mirrors", wantErr: "overlaps /var/lib/buildkite/git-mirrors"},
		{spec: "/srv/x:/etc/buildkite-agent/jwks.json", wantErr: "overlaps /etc/buildkite-agent/jwks.json"},
		{spec: "/srv/x:/etc/buildkite-agent/hooks/pre-command", wantErr: "overlaps /etc/buildkite-agent/hooks"},
		{spec: "/srv/x:/buildkite-agent/bin", wantErr: "overlaps /buildkite-agent/bin"},
		{spec: "/srv/x:/etc/passwd", wantErr: "overlaps /etc/passwd"},
		{spec: "/srv/x:/etc/group", wantErr: "overlaps /etc/group"},
	}
	for _, tc := range tests {
		t.Run(tc.spec, func(t *testing.T) {
			t.Parallel()
			cfg := validConfig()
			cfg.Mounts = []string{tc.spec}
			mounts, _, err := cfg.validate(noEnv)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("validate() error = %v, want an error containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("validate() error = %v", err)
			}
			if diff := cmp.Diff([]mount.Mount{tc.want}, mounts); diff != "" {
				t.Errorf("validate() mounts diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConfigValidate_RejectsDuplicateMountTargets(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Mounts = []string{"/srv/a:/cache", "/srv/b:/cache/"}
	if _, _, err := cfg.validate(noEnv); err == nil || !strings.Contains(err.Error(), "another mount already targets /cache") {
		t.Fatalf("validate() error = %v, want an error about the duplicate target", err)
	}
}

func TestConfigValidate_Env(t *testing.T) {
	t.Parallel()

	lookup := func(name string) (string, bool) {
		if name == "HTTP_PROXY" {
			return "http://proxy:3128", true
		}
		return "", false
	}
	tests := []struct {
		name    string
		env     []string
		want    []string
		wantErr string
	}{
		{name: "value", env: []string{"FOO=bar"}, want: []string{"FOO=bar"}},
		{name: "value_with_equals", env: []string{"FOO=a=b"}, want: []string{"FOO=a=b"}},
		{name: "empty_value", env: []string{"FOO="}, want: []string{"FOO="}},
		{name: "copied_from_agent", env: []string{"HTTP_PROXY"}, want: []string{"HTTP_PROXY=http://proxy:3128"}},
		{name: "unset_in_agent", env: []string{"NO_PROXY"}, wantErr: "not set in the agent's environment"},
		{name: "keeps_order", env: []string{"B=2", "A=1"}, want: []string{"B=2", "A=1"}},
		{name: "missing_name", env: []string{"=x"}, wantErr: "invalid name"},
		{name: "invalid_name", env: []string{"A B=x"}, wantErr: "invalid name"},
		{name: "duplicate", env: []string{"A=1", "A=2"}, wantErr: "set more than once"},
		{name: "reserved_home", env: []string{"HOME=/root"}, wantErr: "set by the executor"},
		{name: "reserved_home_name_only", env: []string{"HOME"}, wantErr: "set by the executor"},
		{name: "reserved_bin_path", env: []string{"BUILDKITE_BIN_PATH"}, wantErr: "set by the executor"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := validConfig()
			cfg.Env = tc.env
			_, env, err := cfg.validate(lookup)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("validate() error = %v, want an error containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("validate() error = %v", err)
			}
			if diff := cmp.Diff(tc.want, env); diff != "" {
				t.Errorf("validate() env diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConfigValidate_RequiredSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "no_image", mutate: func(c *Config) { c.Image = "" }, wantErr: "executor-docker-image is required"},
		{name: "relative_build_path", mutate: func(c *Config) { c.BuildPath = "builds" }, wantErr: "build-path must be an absolute path"},
		{name: "empty_job_context_dir", mutate: func(c *Config) { c.JobContextDir = "" }, wantErr: "job-context-dir is required"},
		{name: "relative_jwks_file", mutate: func(c *Config) { c.SigningJWKSFile = "jwks.json" }, wantErr: "signing-jwks-file must be an absolute path"},
		{name: "relative_mirrors_path", mutate: func(c *Config) { c.GitMirrorsPath = "mirrors" }, wantErr: "git-mirrors-path must be an absolute path"},
		{name: "relative_hooks_path", mutate: func(c *Config) { c.HooksPaths = []string{"hooks"} }, wantErr: "hooks-path and additional-hooks-paths must be absolute"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := validConfig()
			tc.mutate(&cfg)
			if _, _, err := cfg.validate(noEnv); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validate() error = %v, want an error containing %q", err, tc.wantErr)
			}
		})
	}

	if _, _, err := validConfig().validate(noEnv); err != nil {
		t.Errorf("validConfig().validate() error = %v", err)
	}
}
