package dockerexec

import (
	"runtime"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestCreateOptions(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.HooksPaths = []string{"/etc/buildkite-agent/hooks", "/opt/missing-hooks", "/etc/buildkite-agent/hooks/"}
	cfg.GitMirrorsPath = "/var/lib/buildkite/git-mirrors"
	cfg.SigningJWKSFile = "/etc/buildkite-agent/jwks.json"
	cfg.Network = "builds"
	cfg.RunInPty = true
	e := &Executor{
		cfg:         cfg,
		imageID:     "sha256:abc",
		agentBinary: "/usr/bin/buildkite-agent",
		mounts:      []mount.Mount{{Type: mount.TypeBind, Source: "/srv/cache", Target: "/cache", ReadOnly: true}},
		env:         []string{"HTTP_PROXY=http://proxy:3128"},
		uid:         1000,
		gid:         1001,
		groups:      []int{999, 4},
		runID:       "run-1",
	}
	exists := func(p string) bool { return p != "/opt/missing-hooks" }

	got := e.createOptions(Request{
		JobID:         "job-1",
		Env:           []string{"BUILDKITE_JOB_ID=job-1", "BUILDKITE_AGENT_ACCESS_TOKEN=tok"},
		JobLogTmpfile: "/tmp/buildkite_job_log123",
	}, "buildkite-job-job-1-abcd", exists)

	useInit := true
	bind := func(p string, ro bool) mount.Mount {
		return mount.Mount{Type: mount.TypeBind, Source: p, Target: p, ReadOnly: ro}
	}
	want := client.ContainerCreateOptions{
		Name:     "buildkite-job-job-1-abcd",
		Platform: &ocispec.Platform{OS: "linux", Architecture: runtime.GOARCH},
		Config: &container.Config{
			Image:      "sha256:abc",
			Entrypoint: []string{"/buildkite-agent/bin/buildkite-agent"},
			Cmd:        []string{"bootstrap"},
			Env: []string{
				"BUILDKITE_AGENT_ACCESS_TOKEN=tok",
				"BUILDKITE_BIN_PATH=/buildkite-agent/bin",
				"BUILDKITE_JOB_ID=job-1",
				"BUILDKITE_JOB_LOG_TMPFILE=/tmp/buildkite_job_log123",
				"HOME=/tmp/buildkite-home",
				"HTTP_PROXY=http://proxy:3128",
			},
			User:       "1000:1001",
			WorkingDir: "/var/lib/buildkite/builds",
			Tty:        true,
			Labels:     map[string]string{"com.buildkite.job-id": "job-1", "com.buildkite.agent-run": "run-1"},
		},
		HostConfig: &container.HostConfig{
			Mounts: []mount.Mount{
				bind("/var/lib/buildkite/builds", false),
				bind("/var/lib/buildkite/plugins", false),
				bind("/var/lib/buildkite/sockets", false),
				bind("/var/lib/buildkite/job-context", false),
				bind("/var/lib/buildkite/git-mirrors", false),
				bind("/etc/buildkite-agent/hooks", true),
				bind("/etc/buildkite-agent/jwks.json", true),
				bind("/tmp/buildkite_job_log123", true),
				bind("/etc/passwd", true),
				bind("/etc/group", true),
				{Type: mount.TypeBind, Source: "/usr/bin/buildkite-agent", Target: "/buildkite-agent/bin/buildkite-agent", ReadOnly: true},
				{Type: mount.TypeBind, Source: "/srv/cache", Target: "/cache", ReadOnly: true},
			},
			Tmpfs:       map[string]string{"/tmp/buildkite-home": "exec,uid=1000,gid=1001,mode=0700"},
			GroupAdd:    []string{"999", "4"},
			SecurityOpt: []string{"no-new-privileges"},
			Init:        &useInit,
			NetworkMode: "builds",
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("createOptions() diff (-want +got):\n%s", diff)
	}
}

func TestCreateOptions_MinimalConfig(t *testing.T) {
	t.Parallel()

	e := &Executor{cfg: validConfig(), imageID: "sha256:abc", agentBinary: "/usr/bin/buildkite-agent"}
	e.cfg.GitMirrorsPath, e.cfg.SigningJWKSFile = "", ""
	got := e.createOptions(Request{JobID: "job-1"}, "name", func(string) bool { return false })

	if got.Config.Tty {
		t.Error("Config.Tty = true, want false when run-in-pty is off")
	}
	if got.HostConfig.NetworkMode != "" {
		t.Errorf("HostConfig.NetworkMode = %q, want empty for Docker's default", got.HostConfig.NetworkMode)
	}
	var targets []string
	for _, m := range got.HostConfig.Mounts {
		targets = append(targets, m.Target)
	}
	wantTargets := []string{
		"/var/lib/buildkite/builds",
		"/var/lib/buildkite/plugins",
		"/var/lib/buildkite/sockets",
		"/var/lib/buildkite/job-context",
		"/etc/passwd",
		"/etc/group",
		"/buildkite-agent/bin/buildkite-agent",
	}
	if diff := cmp.Diff(wantTargets, targets); diff != "" {
		t.Errorf("mount targets diff (-want +got):\n%s", diff)
	}
}

func TestContainerEnv(t *testing.T) {
	t.Parallel()

	got := containerEnv(
		[]string{"HTTP_PROXY=http://operator", "SHARED=operator", "PATH=/operator/bin"},
		[]string{
			"SHARED=job",
			"DOCKER_HOST=tcp://evil:2375",
			"HOME=/root",
			"BUILDKITE_BIN_PATH=/usr/bin",
			"BUILDKITE_AGENT_PID=42",
			"BUILDKITE_CONFIG_PATH=/etc/buildkite-agent/buildkite-agent.cfg",
			"MULTILINE=a\nb=c",
			"EMPTY=",
			"TWICE=first",
			"TWICE=second",
			"BUILDKITE_JOB_LOG_TMPFILE=/host/only/path",
			"not-an-entry",
			"=no-key",
		},
		"",
	)
	// A job's DOCKER_HOST is ordinary job env: it reaches the container but
	// never the executor's own daemon client.
	want := []string{
		"BUILDKITE_BIN_PATH=/buildkite-agent/bin",
		"DOCKER_HOST=tcp://evil:2375",
		"EMPTY=",
		"HOME=/tmp/buildkite-home",
		"HTTP_PROXY=http://operator",
		"MULTILINE=a\nb=c",
		"PATH=/operator/bin",
		"SHARED=job",
		"TWICE=second",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("containerEnv() diff (-want +got):\n%s", diff)
	}
}
