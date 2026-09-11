package vmsandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/logger"
)

func testSandbox(t *testing.T) *Sandbox {
	t.Helper()
	dir := t.TempDir()
	for _, d := range []string{"bin", "images", "state"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &Sandbox{
		conf:   Config{Dir: dir, ShutdownTimeout: 5 * time.Second},
		logger: logger.Discard,
	}
}

func TestPoisonBlocksSandbox(t *testing.T) {
	t.Parallel()
	s := testSandbox(t)

	if err := s.checkPoison(); err != nil {
		t.Fatalf("checkPoison on fresh sandbox = %v, want nil", err)
	}

	s.poison("firecracker pid 42 would not die")

	err := s.checkPoison()
	if err == nil {
		t.Fatal("checkPoison after poison = nil, want error")
	}
	for _, want := range []string{"firecracker pid 42 would not die", s.conf.poisonPath()} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("checkPoison error %q does not mention %q", err, want)
		}
	}

	// Sweep must not "helpfully" clear the marker: only an operator does.
	if err := s.Sweep(); err != nil {
		t.Fatalf("Sweep = %v", err)
	}
	if _, err := os.Stat(s.conf.poisonPath()); err != nil {
		t.Errorf("poison marker removed by Sweep: %v", err)
	}
}

// startFakeFirecracker copies a binary that blocks forever into
// <Dir>/bin/firecracker and starts it with an argument inside jobDir, the
// way the Runner launches the real thing. It's a real, detached process so
// that Sweep is exercised against /proc rather than a mock.
func startFakeFirecracker(t *testing.T, s *Sandbox, jobDir string) *os.Process {
	t.Helper()
	tailPath, err := exec.LookPath("tail")
	if err != nil {
		t.Skip("tail not available:", err)
	}
	bin, err := os.ReadFile(tailPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.conf.firecrackerPath(), bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	follow := filepath.Join(jobDir, "console.log")
	if err := os.WriteFile(follow, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(s.conf.firecrackerPath(), "-f", follow)
	cmd.SysProcAttr = detachedProc()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	if err := os.WriteFile(filepath.Join(jobDir, "firecracker.pid"), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return cmd.Process
}

func TestSweepKillsOrphanedFirecracker(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("Sweep identifies processes via /proc; Linux only")
	}
	s := testSandbox(t)
	jobDir := filepath.Join(s.conf.stateDir(), "job-orphan")
	proc := startFakeFirecracker(t, s, jobDir)

	if err := s.Sweep(); err != nil {
		t.Fatalf("Sweep = %v", err)
	}

	// We're the parent, so reap it and check it was killed rather than
	// exiting on its own.
	state, err := proc.Wait()
	if err != nil {
		t.Fatalf("Wait = %v", err)
	}
	if ws, ok := state.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Errorf("fake firecracker exit = %v, want killed by SIGKILL", state)
	}
	if _, err := os.Stat(jobDir); !os.IsNotExist(err) {
		t.Errorf("job dir still present after Sweep: %v", err)
	}
	if _, err := os.Stat(s.conf.poisonPath()); !os.IsNotExist(err) {
		t.Errorf("successful Sweep left poison marker: %v", err)
	}
}

func TestSweepLeavesUnrelatedProcessesAlone(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("Sweep identifies processes via /proc; Linux only")
	}
	s := testSandbox(t)

	// A pidfile naming a live process that isn't a firecracker launched for
	// this job dir (a recycled PID). Use ourselves: if Sweep gets this
	// wrong, the test binary dies, which is hard to miss.
	recycled := filepath.Join(s.conf.stateDir(), "job-recycled")
	if err := os.MkdirAll(recycled, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recycled, "firecracker.pid"), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}

	// A pidfile for a process that no longer exists at all.
	dead := filepath.Join(s.conf.stateDir(), "job-dead")
	if err := os.MkdirAll(dead, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dead, "firecracker.pid"), []byte("4194303"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A job dir with no pidfile (crashed before Firecracker started).
	early := filepath.Join(s.conf.stateDir(), "job-early")
	if err := os.MkdirAll(early, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := s.Sweep(); err != nil {
		t.Fatalf("Sweep = %v", err)
	}
	for _, d := range []string{recycled, dead, early} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("%s still present after Sweep: %v", d, err)
		}
	}
}

func TestIsOurFirecrackerRequiresBothBinaryAndDir(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("needs /proc")
	}
	s := testSandbox(t)
	jobDir := filepath.Join(s.conf.stateDir(), "job-a")
	proc := startFakeFirecracker(t, s, jobDir)

	if !isOurFirecracker(proc.Pid, s.conf.firecrackerPath(), jobDir) {
		t.Error("isOurFirecracker(matching pid) = false, want true")
	}
	if isOurFirecracker(proc.Pid, s.conf.firecrackerPath(), filepath.Join(s.conf.stateDir(), "job-b")) {
		t.Error("isOurFirecracker(other job dir) = true, want false")
	}
	if isOurFirecracker(proc.Pid, "/nonexistent/firecracker", jobDir) {
		t.Error("isOurFirecracker(other binary) = true, want false")
	}
	if isOurFirecracker(os.Getpid(), s.conf.firecrackerPath(), jobDir) {
		t.Error("isOurFirecracker(self) = true, want false")
	}
}
