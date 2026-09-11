//go:build linux

package vmsandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/logger"
)

// These tests boot real Firecracker microVMs. They need an installed
// sandbox directory (see guest/README.md) and run only when
// BUILDKITE_VMSANDBOX_INTEGRATION_DIR points at it, e.g.
//
//	BUILDKITE_VMSANDBOX_INTEGRATION_DIR=/opt/bko-sandbox go test -run Integration -v ./internal/vmsandbox/
//
// They share one sandbox directory, so they don't run in parallel: each
// Sandbox.New sweeps the state directory.

const (
	integrationDirEnv = "BUILDKITE_VMSANDBOX_INTEGRATION_DIR"
	crashHarnessEnv   = "BUILDKITE_VMSANDBOX_CRASH_HARNESS_JOB"

	// A small public repository, so checkout is exercised without any
	// credentials.
	testRepo = "https://github.com/buildkite/bash-example.git"
)

func integrationSandbox(t *testing.T) *Sandbox {
	t.Helper()
	dir := os.Getenv(integrationDirEnv)
	if dir == "" {
		t.Skipf("%s not set", integrationDirEnv)
	}
	tap, err := ParseTap("bko-tap0:172.16.0.1/30:172.16.0.2")
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(logger.NewConsoleLogger(logger.NewTextPrinter(os.Stderr), func(int) {}), Config{
		Dir:             dir,
		VCPUs:           2,
		MemoryMiB:       2048,
		Tap:             tap,
		BootTimeout:     90 * time.Second,
		ShutdownTimeout: 15 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// bootstrapEnv is the env the job runner would compute for a token-free
// job that clones testRepo and runs command. Host paths are deliberately
// nonsense: GuestEnv must replace them.
func bootstrapEnv(jobID, command string) []string {
	return []string{
		"BUILDKITE_JOB_ID=" + jobID,
		"BUILDKITE_COMMAND=" + command,
		"BUILDKITE_REPO=" + testRepo,
		"BUILDKITE_COMMIT=HEAD",
		"BUILDKITE_BRANCH=main",
		"BUILDKITE_PIPELINE_PROVIDER=github",
		"BUILDKITE_PULL_REQUEST=false",
		"BUILDKITE_AGENT_NAME=vm-sandbox-test",
		"BUILDKITE_ORGANIZATION_SLUG=bko",
		"BUILDKITE_PIPELINE_SLUG=sandbox",
		"BUILDKITE_BOOTSTRAP_PHASES=plugin,checkout,command",
		"BUILDKITE_BUILD_PATH=/nonexistent/host/builds",
		"BUILDKITE_HOOKS_PATH=/nonexistent/host/hooks",
		"BUILDKITE_PLUGINS_PATH=/nonexistent/host/plugins",
		"BUILDKITE_SOCKETS_PATH=/nonexistent/host/sockets",
		"BUILDKITE_CANCEL_SIGNAL=SIGTERM",
		"BUILDKITE_CANCEL_SIGNAL_TIMEOUT=5s",
	}
}

// syncBuffer collects job output from the runner's goroutines.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func runIntegrationJob(t *testing.T, ctx context.Context, s *Sandbox, jobID string, env []string) (*Runner, string) {
	t.Helper()
	var out syncBuffer
	r := s.NewRunner(RunnerConfig{
		JobID:             jobID,
		Env:               env,
		Stdout:            &out,
		SignalGracePeriod: 5 * time.Second,
	})
	start := time.Now()
	err := r.Run(ctx)
	t.Logf("job %s: Run returned after %v (err=%v), exit status %d", jobID, time.Since(start).Round(time.Millisecond), err, r.WaitStatus().ExitStatus())
	if err != nil {
		t.Fatalf("Run: %v\n--- job output ---\n%s", err, out.String())
	}
	return r, out.String()
}

func assertTornDown(t *testing.T, s *Sandbox, jobID string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(s.conf.stateDir(), jobID)); !os.IsNotExist(err) {
		t.Errorf("state dir for %s still present after Run: %v", jobID, err)
	}
	if err := s.checkPoison(); err != nil {
		t.Errorf("sandbox poisoned: %v", err)
	}
}

// TestIntegration_BootstrapInGuest runs a job whose command proves it's in
// a guest (not this host), uses Docker with a bind mount of the checkout,
// receives a multiline secret without it appearing in the log, and exits
// non-zero. Then it runs a second job and checks nothing from the first
// job survived.
func TestIntegration_BootstrapInGuest(t *testing.T) {
	s := integrationSandbox(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	hostname, _ := os.Hostname()
	const secret = "s3cret-line-one\nline two has spaces and a $dollar\n\tand a tab"

	// The command runs inside bootstrap's checkout of testRepo.
	command := strings.Join([]string{
		`echo "GUEST_HOSTNAME=$(hostname) GUEST_PID1=$(tr '\0' ' ' < /proc/1/cmdline)"`,
		`echo "GUEST_UNAME=$(uname -r) NPROC=$(nproc)"`,
		`echo "CHECKOUT_PWD=$PWD"; test -f README.md && echo CHECKOUT_OK`,
		`echo "SECRET_LEN=${#SANDBOX_TEST_SECRET}"`,
		`echo "$SANDBOX_TEST_SECRET" | grep -c "" | sed 's/^/SECRET_LINES=/'`,
		`echo hello-from-job > marker.txt`,
		`docker run --rm -v "$PWD:/work" -w /work alpine:3.20 sh -c 'echo "IN_CONTAINER=$(cat marker.txt) $(uname -r)"; echo from-container > from-container.txt'`,
		`echo "BIND_MOUNT=$(cat from-container.txt)"`,
		`echo "IMAGES=$(docker images -q | wc -l)"`,
		// The docker plugin uses process substitution, which needs /dev/fd.
		`echo "PROCSUB=$(cat <(echo via-dev-fd))"; echo to-dev-stdout > /dev/stdout`,
		`exit 3`,
	}, "\n")

	env := append(bootstrapEnv("it-job-1", command), "SANDBOX_TEST_SECRET="+secret)
	r, out := runIntegrationJob(t, ctx, s, "it-job-1", env)
	t.Logf("--- job 1 output ---\n%s", out)

	if got := r.WaitStatus().ExitStatus(); got != 3 {
		t.Errorf("exit status = %d, want 3", got)
	}
	if r.WaitStatus().Signaled() {
		t.Error("WaitStatus reports signaled; want a plain exit")
	}
	for _, want := range []string{
		"GUEST_HOSTNAME=bko-sandbox",
		"GUEST_PID1=/bin/bash /sbin/init",
		"GUEST_UNAME=6.1.186",
		"NPROC=2",
		"CHECKOUT_PWD=" + GuestBuildPath + "/vm-sandbox-test/bko/sandbox",
		"CHECKOUT_OK",
		"SECRET_LEN=" + strconv.Itoa(len(secret)),
		"SECRET_LINES=3",
		"IN_CONTAINER=hello-from-job 6.1.186",
		"BIND_MOUNT=from-container",
		"IMAGES=1",
		"PROCSUB=via-dev-fd",
		"to-dev-stdout",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("job output missing %q", want)
		}
	}
	if hostname != "" && strings.Contains(out, "GUEST_HOSTNAME="+hostname) {
		t.Errorf("job ran on the host (%s), not in a guest", hostname)
	}
	for _, line := range strings.Split(secret, "\n") {
		if strings.Contains(out, strings.TrimSpace(line)) {
			t.Errorf("secret line %q appears in job output", line)
		}
	}
	assertTornDown(t, s, "it-job-1")

	// Second job: same agent/org/pipeline so the checkout path is the same.
	// Fresh disk means no marker, no images, and git has to clone again.
	command2 := strings.Join([]string{
		`test ! -e marker.txt && test ! -e from-container.txt && echo WORKSPACE_FRESH`,
		`test -z "$(docker images -q)" && test -z "$(docker ps -aq)" && echo DOCKER_FRESH`,
		`test ! -e /var/lib/buildkite-agent/job-context/marker && echo CONTEXT_FRESH`,
	}, "\n")
	r2, out2 := runIntegrationJob(t, ctx, s, "it-job-2", bootstrapEnv("it-job-2", command2))
	t.Logf("--- job 2 output ---\n%s", out2)
	if got := r2.WaitStatus().ExitStatus(); got != 0 {
		t.Errorf("job 2 exit status = %d, want 0", got)
	}
	for _, want := range []string{"WORKSPACE_FRESH", "DOCKER_FRESH", "CONTEXT_FRESH"} {
		if !strings.Contains(out2, want) {
			t.Errorf("job 2 output missing %q", want)
		}
	}
	// bootstrap prints this when it clones rather than fetches into an
	// existing checkout.
	if !strings.Contains(out2, "Cloning into") {
		t.Errorf("job 2 did not clone afresh")
	}
	assertTornDown(t, s, "it-job-2")
}

// TestIntegration_CancelUncooperativeGuest cancels a job whose command has
// frozen the guest helper (SIGSTOP), so the interrupt can't be acted on.
// The host must terminate the VM itself after the grace period and still
// tear down cleanly.
func TestIntegration_CancelUncooperativeGuest(t *testing.T) {
	s := integrationSandbox(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// BUILDKITE_AGENT_PID inside the guest is the helper's PID.
	command := strings.Join([]string{
		`echo "FREEZING helper pid $BUILDKITE_AGENT_PID"`,
		`kill -STOP "$BUILDKITE_AGENT_PID"`,
		`sleep 600`,
	}, "\n")

	var out syncBuffer
	r := s.NewRunner(RunnerConfig{
		JobID:             "it-job-cancel",
		Env:               append(bootstrapEnv("it-job-cancel", command), "BUILDKITE_BOOTSTRAP_PHASES=command"),
		Stdout:            &out,
		SignalGracePeriod: 5 * time.Second,
	})

	jobCtx, cancelJob := context.WithCancel(ctx)
	defer cancelJob()
	done := make(chan error, 1)
	go func() { done <- r.Run(jobCtx) }()

	// Wait for the command to have run (the helper is frozen right after
	// this line is emitted).
	deadline := time.Now().Add(3 * time.Minute)
	for !strings.Contains(out.String(), "FREEZING helper pid") {
		if time.Now().After(deadline) {
			t.Fatalf("job never started:\n%s", out.String())
		}
		time.Sleep(200 * time.Millisecond)
	}
	time.Sleep(time.Second) // let the STOP land

	cancelled := time.Now()
	cancelJob()
	select {
	case err := <-done:
		t.Logf("Run returned %v after %v", err, time.Since(cancelled).Round(time.Millisecond))
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(2 * time.Minute):
		t.Fatalf("Run did not return after cancellation:\n%s", out.String())
	}
	if took := time.Since(cancelled); took > 60*time.Second {
		t.Errorf("cancellation took %v, want bounded by grace (5s) + shutdown timeout (15s) plus slack", took)
	}

	ws := r.WaitStatus()
	if !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Errorf("WaitStatus = %+v, want killed by SIGKILL", ws)
	}
	t.Logf("--- job output ---\n%s", out.String())
	assertTornDown(t, s, "it-job-cancel")
}

// TestIntegration_AgentCrashRecovery kills the process supervising a running
// job (a re-exec of this test binary) with SIGKILL, then checks that the
// orphaned guest powers itself off once it loses the host, and that a new
// Sandbox sweeps the leftover state before accepting work.
func TestIntegration_AgentCrashRecovery(t *testing.T) {
	s := integrationSandbox(t)
	const jobID = "it-job-crash"
	jobDir := filepath.Join(s.conf.stateDir(), jobID)

	harness := exec.Command(os.Args[0], "-test.run=^TestIntegration_CrashHarness$", "-test.v")
	harness.Env = append(os.Environ(), crashHarnessEnv+"="+jobID)
	var harnessOut syncBuffer
	harness.Stdout = &harnessOut
	harness.Stderr = &harnessOut
	if err := harness.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = harness.Process.Kill(); _ = harness.Wait() }()

	deadline := time.Now().Add(3 * time.Minute)
	for !strings.Contains(harnessOut.String(), "HARNESS_JOB_RUNNING") {
		if time.Now().After(deadline) {
			t.Fatalf("harness job never started:\n%s", harnessOut.String())
		}
		time.Sleep(200 * time.Millisecond)
	}
	pidBytes, err := os.ReadFile(filepath.Join(jobDir, "firecracker.pid"))
	if err != nil {
		t.Fatalf("reading pidfile: %v", err)
	}
	fcPid, _ := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if !isOurFirecracker(fcPid, s.conf.firecrackerPath(), jobDir) {
		t.Fatalf("pid %d is not a firecracker for %s", fcPid, jobDir)
	}

	// Abrupt "agent" death.
	if err := harness.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = harness.Wait()
	killedAt := time.Now()

	// Firecracker was started in its own session, so it survives its
	// parent. The guest, however, loses its vsock connection and must power
	// off on its own.
	if !processRunning(fcPid) && time.Since(killedAt) < 100*time.Millisecond {
		t.Log("firecracker exited immediately with the harness (unexpected but acceptable)")
	}
	for processRunning(fcPid) {
		if time.Since(killedAt) > 60*time.Second {
			t.Errorf("firecracker pid %d still running %v after supervisor died; guest did not self-power-off", fcPid, time.Since(killedAt))
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("firecracker gone %v after supervisor was killed", time.Since(killedAt).Round(time.Millisecond))

	if _, err := os.Stat(jobDir); err != nil {
		t.Fatalf("expected orphaned state dir %s to remain until swept: %v", jobDir, err)
	}

	// Next agent start: New sweeps. If firecracker were somehow still
	// alive, Sweep would kill it (covered by TestSweepKillsOrphanedFirecracker).
	s2 := integrationSandbox(t)
	if _, err := os.Stat(jobDir); !os.IsNotExist(err) {
		t.Errorf("state dir %s not swept by New: %v", jobDir, err)
	}
	if err := s2.checkPoison(); err != nil {
		t.Errorf("sandbox poisoned after recovery: %v", err)
	}
	t.Logf("--- harness output ---\n%s", harnessOut.String())
}

// TestIntegration_CrashHarness is the child process for
// TestIntegration_AgentCrashRecovery. It runs a long job and expects to be
// killed.
func TestIntegration_CrashHarness(t *testing.T) {
	jobID := os.Getenv(crashHarnessEnv)
	if jobID == "" {
		t.Skip("not running as crash harness")
	}
	s := integrationSandbox(t)
	command := `echo HARNESS_JOB_RUNNING; sleep 600`
	r := s.NewRunner(RunnerConfig{
		JobID:  jobID,
		Env:    append(bootstrapEnv(jobID, command), "BUILDKITE_BOOTSTRAP_PHASES=command"),
		Stdout: os.Stdout,
	})
	if err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("harness job finished with %d; it should have been killed", r.WaitStatus().ExitStatus())
}

// TestFirecrackerConfigCarriesNoJobEnv checks the only per-job file on the
// host disk and the VMM's argv don't carry the job environment: it travels
// over vsock only.
func TestFirecrackerConfigCarriesNoJobEnv(t *testing.T) {
	t.Parallel()
	s := testSandbox(t)
	tap, _ := ParseTap("tap0:10.0.0.1/30:10.0.0.2")
	s.conf.Tap = tap
	s.conf.DNS = []string{"10.0.0.1"}
	r := s.NewRunner(RunnerConfig{JobID: "j", Env: []string{"SECRET=hunter2", "BUILDKITE_COMMAND=echo hunter2"}})
	cfg := fmt.Sprint(r.firecrackerConfig())
	if strings.Contains(cfg, "hunter2") {
		t.Errorf("firecracker config contains job env: %s", cfg)
	}
	for _, want := range []string{"ip=10.0.0.2::10.0.0.1:255.255.255.252::eth0:off", "bko_dns=10.0.0.1", "tap0"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("firecracker config missing %q: %s", want, cfg)
		}
	}
}
