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
	s := newJobStatus(&statusTrailer{w: &out}, make(chan struct{}))

	// Hooks wrap their exit status, which is -1 for a signal.
	err := exec.Command("sh", "-c", "kill -KILL $$").Run()
	s.start("hook/agent/pre-command", "Running agent pre-command hook")
	s.finish("hook/agent/pre-command", &shell.ExitError{Code: -1, Err: hookExitError{message: "the hook exited with status -1", err: err}}, "The agent pre-command hook")
	s.clearDone() // flushes the held reports

	var last osc7501.Report
	scanner := osc7501.Scanner{W: io.Discard, OnReport: func(r osc7501.Report) { last = r }}
	_, _ = scanner.Write(out.Bytes())
	if want := "The agent pre-command hook was terminated by signal SIGKILL"; last.State != osc7501.StateError || last.Msg != want {
		t.Errorf("last report = %s %q, want %s %q", last.State, last.Msg, osc7501.StateError, want)
	}
}

func TestStatusTrailer(t *testing.T) {
	t.Parallel()
	const rep = "\x1b]7501;x\x1b\\"

	tests := []struct {
		name      string
		writes    []string
		noPending bool
		flush     bool
		want      string
	}{
		{name: "header", writes: []string{"~~~ Running x\n"}, want: "~~~ Running x" + rep + "\n"},
		{name: "skips ^^^ +++", writes: []string{"^^^ +++\n", "~~~ y\n"}, want: "^^^ +++\n~~~ y" + rep + "\n"},
		{name: "error before ^^^ +++", writes: []string{"🚨 Error: x\n^^^ +++\n"}, want: "🚨 Error: x" + rep + "\n^^^ +++\n"},
		{name: "crlf", writes: []string{"x\r\n"}, want: "x" + rep + "\r\n"},
		{name: "no newline keeps holding", writes: []string{"partial"}, want: "partial"},
		{name: "no newline then line", writes: []string{"partial", " more\n"}, want: "partial more" + rep + "\n"},
		{name: "no pending passes through", writes: []string{"a\n"}, noPending: true, want: "a\n"},
		{name: "flush with pending", flush: true, want: rep},
		{name: "flush without pending", noPending: true, flush: true, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			tr := &statusTrailer{w: &out}
			if !test.noPending {
				tr.hold([]byte(rep))
			}
			for _, w := range test.writes {
				n, err := tr.Write([]byte(w))
				if err != nil || n != len(w) {
					t.Fatalf("Write(%q) = %d, %v, want %d, nil", w, n, err, len(w))
				}
			}
			if test.flush {
				if err := tr.Flush(); err != nil {
					t.Fatalf("Flush() error = %v", err)
				}
			}
			if got := out.String(); got != test.want {
				t.Errorf("output = %q, want %q", got, test.want)
			}
		})
	}
}
