package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/api"
	envutil "github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/dockerbootstrap"
)

func TestDockerBootstrapStepRejection(t *testing.T) {
	for _, image := range []string{`"ubuntu"`, `""`, `null`, `42`, `{}`} {
		for _, script := range []string{"/usr/bin/buildkite-agent docker-bootstrap", "/usr/bin/buildkite-agent bootstrap"} {
			t.Run(script+image, func(t *testing.T) {
				var job api.Job
				if err := json.Unmarshal([]byte(`{"step":{"command":"true","image":`+image+`}}`), &job); err != nil {
					t.Fatal(err)
				}
				conf := JobRunnerConfig{Job: &job, AgentConfiguration: AgentConfiguration{BootstrapScript: script}}
				err := validateDockerBootstrapStep(conf)
				if want := strings.Contains(script, "docker-bootstrap"); (err != nil) != want {
					t.Fatalf("rejection=%v, want %t", err, want)
				}
			})
		}
	}
	conf := JobRunnerConfig{Job: &api.Job{}, AgentConfiguration: AgentConfiguration{BootstrapScript: "buildkite-agent docker-bootstrap"}}
	if err := validateDockerBootstrapStep(conf); err != nil {
		t.Fatal(err)
	}
}

func TestDockerJobContextIsolation(t *testing.T) {
	base := filepath.Join(t.TempDir(), "agent-context")
	first, err := createDockerJobContext(base)
	if err != nil {
		t.Fatal(err)
	}
	second, err := createDockerJobContext(base)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || filepath.Dir(first) != base || filepath.Dir(second) != base {
		t.Fatal("job contexts not isolated")
	}
	info, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	// Windows synthesizes mode bits; they do not describe directory ACLs.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatal("job context accessible to other users")
	}
	for _, invalid := range []string{"", ".", "/", "/tmp", os.TempDir()} {
		if _, err := createDockerJobContext(invalid); err == nil {
			t.Errorf("accepted context root %q", invalid)
		}
	}
}

func TestDockerContextCannotBeSpoofedByJobEnvironment(t *testing.T) {
	r := controlPlaneTestRunner(t, map[string]string{dockerbootstrap.ContextEnv: "/tmp/other-job"}, AgentConfiguration{})
	r.dockerContextDir = "/private/real-job"
	got, err := r.createEnvironment(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	environ := envutil.FromSlice(got)
	if value, _ := environ.Get(dockerbootstrap.ContextEnv); value != r.dockerContextDir {
		t.Fatal("job overrode private context")
	}
	ignored, _ := environ.Get("BUILDKITE_IGNORED_ENV")
	if !strings.Contains(ignored, dockerbootstrap.ContextEnv) {
		t.Fatal("spoofed value not recorded as ignored")
	}
}
