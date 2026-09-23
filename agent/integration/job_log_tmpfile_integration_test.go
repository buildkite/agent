package integration

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/agent"
	"github.com/buildkite/agent/v4/api"
)

// TestJobLogTmpfileIsClosedAndRemoved asserts that when EnableJobLogTmpfile is
// set, the job log temp file is closed before it is removed. Closing matters:
// on Windows os.Remove fails while the file is still open, leaving orphaned
// temp files; on Unix the remove succeeds but the descriptor stays open in the
// agent process and long-running agents leak one fd per job.
func TestJobLogTmpfileIsClosedAndRemoved(t *testing.T) {
	if runtime.GOOS == "windows" {
		// This test inspects open files via /proc/self/fd, which only exists
		// on Unix. On Windows the same root cause shows up as a leftover
		// file instead of a leaked descriptor.
		t.Skip("fd inspection via /proc is unix-only")
	}

	t.Parallel()
	ctx := t.Context()

	jobLogDir := t.TempDir()

	j := &api.Job{
		ID:                 "my-job-id",
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_COMMAND": "echo hello world",
		},
		Token: "bkaj_job-token",
	}

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t

	mb.Expect().Once().AndExitWith(0)

	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	err := runJob(t, ctx, testRunJobConfig{
		job:    j,
		server: server,
		agentCfg: agent.AgentConfiguration{
			EnableJobLogTmpfile: true,
			JobLogPath:          jobLogDir,
		},
		mockBootstrap: mb,
	})
	if err != nil {
		t.Fatalf("runJob() error = %v", err)
	}

	// The cleanup goroutine removes the temp file after the process exits,
	// so wait for the file to disappear before inspecting descriptors.
	waitForJobLogTmpfileRemoval(t, jobLogDir)

	// After cleanup, no descriptor in this process may still reference the
	// job log temp file. An unclosed-but-removed file shows up in
	// /proc/self/fd with a "(deleted)" suffix.
	fds, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatalf("os.ReadDir(/proc/self/fd) error = %v", err)
	}
	for _, fd := range fds {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", fd.Name()))
		if err != nil {
			continue
		}
		if strings.Contains(target, "buildkite_job_log") {
			t.Errorf("leaked file descriptor for job log temp file: fd %s -> %s", fd.Name(), target)
		}
	}
}

func waitForJobLogTmpfileRemoval(t *testing.T, dir string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("os.ReadDir(%q) error = %v", dir, err)
		}
		leftovers := 0
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "buildkite_job_log") {
				leftovers++
			}
		}
		if leftovers == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for job log temp file removal in %s: %d leftover file(s)", dir, leftovers)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
