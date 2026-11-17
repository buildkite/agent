package job

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/tracetools"
	"github.com/buildkite/osc7501"
)

func TestProgramStatusSpanIDs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script hooks aren't supported on Windows")
	}

	for _, test := range []struct {
		name string
		hook HookConfig
		id   string
	}{
		{name: "command", id: "command"},
		{name: "agent hook", hook: HookConfig{Scope: HookScopeAgent, Name: "pre-command"}, id: "hook/agent/pre-command"},
		{name: "repository hook", hook: HookConfig{Scope: HookScopeRepository, Name: "post-command"}, id: "hook/repository/post-command"},
		{name: "plugin hook", hook: HookConfig{Scope: HookScopePlugin, Name: "command", PluginID: "docker", PluginName: "docker#v5.0.0"}, id: "hook/plugin/docker/command"},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := installGlobalSpanRecorder(t)
			var out bytes.Buffer
			trailer := &statusTrailer{w: &out}
			sh, err := shell.New(shell.WithLogger(shell.NewWriterLogger(trailer, false, nil)), shell.WithStdout(io.Discard))
			if err != nil {
				t.Fatalf("shell.New() error = %v", err)
			}
			e := New(ExecutorConfig{
				TracingBackend: tracetools.BackendOpenTelemetry,
				Command:        "echo hello",
				CommandEval:    true,
				Shell:          "/bin/sh -c",
				HooksPath:      t.TempDir(),
			})
			e.shell = sh
			e.status = newJobStatus(trailer, e.cancelCh)

			spanName := "command"
			if test.hook.Name == "" {
				if hookErr, commandErr := e.CommandPhase(t.Context()); hookErr != nil || commandErr != nil {
					t.Fatalf("CommandPhase() errors = %v, %v", hookErr, commandErr)
				}
			} else {
				hookCfg := test.hook
				hookCfg.Path = filepath.Join(t.TempDir(), "hook")
				hookCfg.Env = e.shell.Env
				if err := os.WriteFile(hookCfg.Path, []byte("#!/usr/bin/env ruby\nexit(0)\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := e.executeHook(t.Context(), hookCfg); err != nil {
					t.Fatalf("executeHook() error = %v", err)
				}
				spanName = hookCfg.Scope + " " + hookCfg.Name + " hook"
			}
			e.status.clearDone()

			span := findOnlySpan(t, recorder.Ended(), spanName)
			assertSpanAttr(t, span, "buildkite.program_status.id", test.id)
			var matched bool
			scanner := osc7501.Scanner{W: io.Discard, OnReport: func(r osc7501.Report) {
				if r.State == osc7501.StateWorking && r.ID == test.id {
					matched = true
				}
			}}
			if _, err := scanner.Write(out.Bytes()); err != nil {
				t.Fatal(err)
			}
			if !matched {
				t.Errorf("no working OSC report for span's program status ID %q in %q", test.id, out.String())
			}
		})
	}
}

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
