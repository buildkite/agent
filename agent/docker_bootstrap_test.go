package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/logger"
	"github.com/buildkite/agent/v4/metrics"

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

func TestUsesDockerBootstrap(t *testing.T) {
	for _, tc := range []struct {
		script           string
		kubernetes, want bool
	}{
		{"/usr/bin/buildkite-agent docker-bootstrap", false, true},
		{"buildkite-agent docker-bootstrap --image example", false, true},
		{"/usr/bin/buildkite-agent docker-bootstrap", true, false},
		{"/usr/bin/wrapper", false, false},
		{"buildkite-agent", false, false},
		{"docker-bootstrap --image example", false, false},
		{"", false, false},
		{"buildkite-agent '", false, false},
	} {
		t.Run(tc.script, func(t *testing.T) {
			conf := JobRunnerConfig{KubernetesExec: tc.kubernetes, AgentConfiguration: AgentConfiguration{BootstrapScript: tc.script}}
			if got := usesDockerBootstrap(conf); got != tc.want {
				t.Fatalf("got %t want %t", got, tc.want)
			}
		})
	}
}

func TestDockerStepRejectionThroughRun(t *testing.T) {
	var finished api.JobFinishRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/jobs/test-job/start":
		case "/jobs/test-job/finish":
			if err := json.NewDecoder(req.Body).Decode(&finished); err != nil {
				t.Error(err)
			}
		default:
			t.Errorf("unexpected request %s", req.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	var job api.Job
	if err := json.Unmarshal([]byte(`{"id":"test-job","env":{},"step":{"command":"true","image":"ubuntu"},"chunks_max_size_bytes":1024}`), &job); err != nil {
		t.Fatal(err)
	}
	conf := JobRunnerConfig{
		Job: &job, JobContextDir: filepath.Join(t.TempDir(), "context"),
		AgentConfiguration: AgentConfiguration{BootstrapScript: "missing-agent docker-bootstrap", BuildPath: t.TempDir()},
		MetricsScope:       metrics.NewCollector(logger.Discard, metrics.CollectorConfig{}).Scope(nil),
	}
	r, err := NewJobRunner(t.Context(), logger.Discard, api.NewClient(logger.Discard, api.Config{Endpoint: server.URL}), conf)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	r.logStreamer = NewLogStreamer(logger.Discard, func(_ context.Context, c *api.Chunk) error { _, err := logs.Write(c.Data); return err }, LogStreamerConfig{Concurrency: 1, MaxChunkSizeBytes: 1024})
	dir := r.dockerContextDir
	if err := os.WriteFile(r.jobTimeoutFilePath, []byte("timeout"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if finished.ExitStatus != "-1" || finished.SignalReason != SignalReasonProcessRunError {
		t.Fatalf("finish: %+v", finished)
	}
	if !strings.Contains(logs.String(), "step image is rejected") {
		t.Fatalf("missing rejection log: %s", logs.String())
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("private context not removed: %v", err)
	}
	select {
	case <-r.process.Started():
		t.Fatal("bootstrap started for rejected job")
	default:
	}
}
