package job

import (
	"bytes"
	"io"
	"os/exec"
	"runtime"
	"testing"

	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/osc7501"
)

func TestJobStatusDescribesSignalledExits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are POSIX")
	}
	t.Parallel()
	var out bytes.Buffer
	s := newJobStatus(&out, make(chan struct{}))

	// Hooks wrap their exit status, which is -1 for a signal.
	err := exec.Command("sh", "-c", "kill -KILL $$").Run()
	s.start("hook/agent/pre-command", "Running agent pre-command hook")
	s.finish("hook/agent/pre-command", &shell.ExitError{Code: -1, Err: hookExitError{message: "the hook exited with status -1", err: err}}, "The agent pre-command hook")

	var last osc7501.Report
	scanner := osc7501.Scanner{W: io.Discard, OnReport: func(r osc7501.Report) { last = r }}
	_, _ = scanner.Write(out.Bytes())
	if want := "The agent pre-command hook was terminated by signal SIGKILL"; last.State != osc7501.StateError || last.Msg != want {
		t.Errorf("last report = %s %q, want %s %q", last.State, last.Msg, osc7501.StateError, want)
	}
}
