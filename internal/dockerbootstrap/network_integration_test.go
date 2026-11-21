//go:build linux

package dockerbootstrap

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDockerNetworkIntegration(t *testing.T) {
	binary := os.Getenv("DOCKER_BOOTSTRAP_TEST_BINARY")
	if binary == "" {
		t.Skip("set DOCKER_BOOTSTRAP_TEST_BINARY to a static Linux agent binary")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	client := CLI{Path: "/usr/bin/docker", ConfigDir: t.TempDir()}
	docker := func(args ...string) string {
		t.Helper()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	type result struct {
		code int
		err  error
	}
	type runningJob struct {
		name, ip, network string
		cancel            context.CancelFunc
		done              chan result
	}
	var jobs []runningJob
	for _, forced := range []bool{false, true} {
		cfg := testConfig(t)
		cfg.Binary, cfg.PullPolicy = binary, "never"
		cfg.CleanupMargin, cfg.OperationTimeout = 3*time.Second, 15*time.Second
		build := t.TempDir()
		handler := "lambda *_: exit(0)"
		if forced {
			handler = "signal.SIG_IGN"
		}
		command := fmt.Sprintf("exec python3 -u - <<'PY'\n"+`
import pathlib
import signal
import socket
import urllib.request
signal.signal(signal.SIGTERM, %s)
signal.signal(signal.SIGINT, %s)
with urllib.request.urlopen("https://example.com", timeout=15) as response:
    assert response.status == 200
server = socket.socket()
server.bind(("0.0.0.0", 8765))
server.listen()
pathlib.Path("ready").touch()
while True:
    connection, _ = server.accept()
    connection.close()
PY
`, handler, handler)
		cfg.Environment = append(cfg.Environment,
			"BUILDKITE_JOB_ID=network-"+filepath.Base(build),
			"BUILDKITE_BUILD_PATH="+build, "BUILDKITE_BUILD_CHECKOUT_PATH="+build,
			"BUILDKITE_BOOTSTRAP_PHASES=command", "BUILDKITE_COMMAND="+command,
			"BUILDKITE_CANCEL_SIGNAL_TIMEOUT=5s", "BUILDKITE_PTY=false",
			"BUILDKITE_REPO=.", "BUILDKITE_COMMIT=HEAD", "BUILDKITE_BRANCH=main",
			"BUILDKITE_PIPELINE_PROVIDER=custom", "BUILDKITE_AGENT_NAME=test-agent",
			"BUILDKITE_ORGANIZATION_SLUG=test-org", "BUILDKITE_PIPELINE_SLUG=test-pipeline")
		log, err := os.CreateTemp(t.TempDir(), "job-log")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = log.Close() })
		jobCtx, jobCancel := context.WithCancel(ctx)
		done := make(chan result, 1)
		go func() {
			code, err := (Runner{Client: client, Stdout: log, Stderr: log}).Run(jobCtx, cfg)
			done <- result{code, err}
			close(done)
		}()
		// Drain supervisors before testing.T removes their bind-mounted fixtures.
		t.Cleanup(func() {
			jobCancel()
			<-done
		})
		ready := filepath.Join(build, "ready")
	waiting:
		for {
			if _, err := os.Stat(ready); err == nil {
				break
			}
			select {
			case result := <-done:
				output, _ := os.ReadFile(log.Name())
				t.Fatalf("job ended before ready: %+v\n%s", result, output)
			case <-ctx.Done():
				t.Fatal("timed out waiting for job readiness")
			case <-time.After(100 * time.Millisecond):
				continue waiting
			}
		}
		output, err := os.ReadFile(log.Name())
		if err != nil {
			t.Fatal(err)
		}
		var network string
		for _, line := range strings.Split(string(output), "\n") {
			if value, ok := strings.CutPrefix(line, "Docker bootstrap network: "); ok {
				network = value
			}
		}
		if !strings.HasPrefix(network, "buildkite_network_") {
			t.Fatalf("network missing from log: %s", output)
		}
		name := strings.Replace(network, "buildkite_network_", "buildkite_job_", 1)
		if got := docker("inspect", "--format", "{{len .NetworkSettings.Networks}}", name); got != "1" {
			t.Fatalf("expected one network, got %s", got)
		}
		if got := docker("inspect", "--format", "{{json .HostConfig.PortBindings}}", name); got != "{}" && got != "null" {
			t.Fatalf("unexpected published ports: %s", got)
		}
		ip := docker("inspect", "--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", name)
		jobs = append(jobs, runningJob{name, ip, network, jobCancel, done})
	}
	if jobs[0].network == jobs[1].network {
		t.Fatal("jobs share a network")
	}
	for i, job := range jobs {
		docker("exec", job.name, "python3", "-c", `import socket; socket.create_connection(("127.0.0.1", 8765), timeout=2).close()`)
		docker("exec", job.name, "python3", "-c", `import socket, sys
try:
    socket.create_connection((sys.argv[1], 8765), timeout=2).close()
except OSError:
    pass
else:
    raise SystemExit("cross-job connection succeeded")
`, jobs[1-i].ip)
	}
	for i, job := range jobs {
		job.cancel()
		result := <-job.done
		if result.err != nil || (i == 0 && result.code != 143 && result.code != 0) || (i == 1 && result.code != 137) {
			t.Errorf("cancellation result: %+v", result)
		}
		if got := docker("ps", "-a", "-q", "--filter", "name=^/"+job.name+"$"); got != "" {
			t.Errorf("container remains: %s", got)
		}
		if got := docker("network", "ls", "-q", "--filter", "name=^"+job.network+"$"); got != "" {
			t.Errorf("network remains: %s", got)
		}
	}
}
