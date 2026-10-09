package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/process"
	"github.com/buildkite/agent/v4/logger"
)

// TestExecExecutor_RunsBootstrapInBuildPathWithJobEnvOverHostEnv checks
// what the bootstrap subprocess sees: the build path as its working
// directory, and the agent's environment underneath the job environment.
func TestExecExecutor_RunsBootstrapInBuildPathWithJobEnvOverHostEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test bootstrap is a POSIX shell script")
	}
	t.Setenv("EXEC_TEST_HOST_ONLY", "from-host")
	t.Setenv("EXEC_TEST_BOTH", "from-host")

	buildPath, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("filepath.EvalSymlinks() error = %v", err)
	}
	exe := &execExecutor{
		logger: logger.Discard,
		bootstrapScript: writeExecTestBootstrap(t, `echo "pwd=$(pwd -P)"
echo "host_only=$EXEC_TEST_HOST_ONLY"
echo "both=$EXEC_TEST_BOTH"
`),
		buildPath: buildPath,
	}
	out := &process.Buffer{}

	execution, err := exe.New(t.Context(), JobExecutionRequest{
		Env:    []string{"EXEC_TEST_BOTH=from-job"},
		Output: out,
	})
	if err != nil {
		t.Fatalf("execExecutor.New() error = %v", err)
	}
	if err := execution.Run(t.Context()); err != nil {
		t.Fatalf("execution.Run() error = %v", err)
	}

	got := string(out.ReadAndTruncate())
	for _, want := range []string{
		"pwd=" + buildPath + "\n",
		"host_only=from-host\n",
		"both=from-job\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("bootstrap output = %q, want it to contain %q", got, want)
		}
	}
}

// TestExecExecutor_InterruptSendsSIGTERMWhenCancelSignalIsSIGKILL checks that
// a SIGKILL cancel signal reaches bootstrap as SIGTERM, so bootstrap can run
// its pre-exit hooks and report the command's exit status.
func TestExecExecutor_InterruptSendsSIGTERMWhenCancelSignalIsSIGKILL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test bootstrap is a POSIX shell script")
	}
	exe := &execExecutor{
		logger: logger.Discard,
		bootstrapScript: writeExecTestBootstrap(t, `trap 'exit 42' TERM
echo ready
while :; do sleep 0.1; done
`),
		buildPath:    t.TempDir(),
		cancelSignal: process.SIGKILL,
	}
	out := &process.Buffer{}

	execution, err := exe.New(t.Context(), JobExecutionRequest{Output: out})
	if err != nil {
		t.Fatalf("execExecutor.New() error = %v", err)
	}
	runErr := make(chan error, 1)
	go func() { runErr <- execution.Run(t.Context()) }()

	var seen strings.Builder
	for deadline := time.Now().Add(10 * time.Second); !strings.Contains(seen.String(), "ready"); {
		if time.Now().After(deadline) {
			t.Fatalf("bootstrap did not become ready, output = %q", seen.String())
		}
		seen.Write(out.ReadAndTruncate())
		time.Sleep(10 * time.Millisecond)
	}

	if err := execution.Interrupt(); err != nil {
		t.Fatalf("execution.Interrupt() error = %v", err)
	}
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("execution.Run() error = %v", err)
		}
	case <-time.After(10 * time.Second):
		_ = execution.Terminate()
		t.Fatal("bootstrap did not exit within 10s of Interrupt")
	}
	if got, want := execution.WaitStatus().ExitStatus(), 42; got != want {
		t.Errorf("execution.WaitStatus().ExitStatus() = %d, want %d (the SIGTERM trap's exit code)", got, want)
	}
}

// writeExecTestBootstrap writes a POSIX shell bootstrap script with the given
// body and returns its path.
func writeExecTestBootstrap(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bootstrap.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
	return path
}
