package job

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/process"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/tracetools"
	"github.com/buildkite/osc7501"
	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
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
		for _, reporting := range []string{"enabled", "disabled"} {
			t.Run(test.name+"/"+reporting, func(t *testing.T) {
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
				if reporting == "enabled" {
					e.status = newJobStatus(trailer, e.cancelCh)
				}

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
				if reporting == "enabled" {
					assertSpanAttr(t, span, "buildkite.program_status.id", test.id)
					msg := "Running the command"
					if test.hook.Name != "" {
						msg = "Running " + test.hook.Scope + " "
						if test.hook.PluginName != "" {
							msg += test.hook.PluginName + " "
						}
						msg += test.hook.Name + " hook"
					}
					assertProgramStatusEvents(t, span, [][3]string{
						{test.id, "working", msg},
						{test.id, "done", ""},
					})
				} else if id, ok := spanAttr(span, "buildkite.program_status.id"); ok {
					t.Errorf("span's program status ID = %q with reporting disabled, want no attribute", id)
				}
				if reporting == "disabled" {
					assertProgramStatusEvents(t, span, nil)
				}
				var matched bool
				scanner := osc7501.Scanner{W: io.Discard, OnReport: func(r osc7501.Report) {
					if r.State == osc7501.StateWorking && r.ID == test.id {
						matched = true
					}
				}}
				if _, err := scanner.Write(out.Bytes()); err != nil {
					t.Fatal(err)
				}
				if want := reporting == "enabled"; matched != want {
					t.Errorf("working OSC report for %q present = %t, want %t; output: %q", test.id, matched, want, out.String())
				}
			})
		}
	}
}

func TestProgramStatusOutputSpanEvents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test commands require a POSIX shell")
	}
	for _, exitCode := range []int{0, 7} {
		t.Run(fmt.Sprint(exitCode), func(t *testing.T) {
			recorder := installGlobalSpanRecorder(t)
			e := New(ExecutorConfig{
				TracingBackend: tracetools.BackendOpenTelemetry,
				CommandEval:    true,
				Shell:          "/bin/sh -c",
				HooksPath:      t.TempDir(),
				RedactedVars:   []string{"MY_SECRET"},
			})
			var stdout, stderr bytes.Buffer
			out, log := e.setupRedactors(shell.DiscardLogger, env.FromMap(map[string]string{"MY_SECRET": "secret-value"}), &stdout, &stderr)
			e.programStatus = process.NewProgramStatusWriter(out)
			e.status = newJobStatus(&statusTrailer{w: log.Writer}, e.cancelCh)
			sh, err := shell.New(shell.WithLogger(log), shell.WithStdout(e.programStatus))
			if err != nil {
				t.Fatal(err)
			}
			e.shell = sh
			// A different record in each hook catches output assigned to the
			// command span or a hook whose context was not restored.
			pre := osc7501.Report{ID: "build/check", State: "blocked", Progress: -1, Msg: "Need secret-value"}
			post := osc7501.Report{ID: "build/cleanup", State: "done", Progress: -1, Msg: "Cleaned up"}
			for name, r := range map[string]osc7501.Report{"pre-command": pre, "post-command": post} {
				if err := os.WriteFile(filepath.Join(e.HooksPath, name), fmt.Appendf(nil, "#!/usr/bin/env ruby\nSTDOUT.write(%q)\n", r.Encode()), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			const token = "bkua_abcdefghijklmnopqrstuvwxyz0123456789"
			r := osc7501.Report{ID: "build/test", State: "error", Progress: -1, Msg: "Test failed: secret-value late-secret " + token}
			e.Command = fmt.Sprintf("printf '%%s' '%s'; exit %d", r.Encode(), exitCode)
			root, ctx := tracetools.StartSpanFromContext(t.Context(), "job", e.TracingBackend)
			restore := e.outputSpan(ctx)
			// Runtime secrets must not be captured only at span start.
			e.redactors.Add("late-secret")
			hookErr, commandErr := e.CommandPhase(ctx)
			if hookErr != nil || shell.ExitCode(commandErr) != exitCode {
				t.Fatalf("CommandPhase() = %v, %v; want exit %d; stdout: %q; stderr: %q", hookErr, commandErr, exitCode, stdout.String(), stderr.String())
			}
			e.status.clearDone()
			// Output after the phase belongs to the job again. Its callback
			// predates the runtime secret registration above.
			for _, report := range []osc7501.Report{
				{State: "idle", Progress: -1, Msg: "Finished late-secret"},
				// Partial secrets must not join across fields or reports.
				{ID: "secret-", State: "working", Progress: -1, Msg: "value"},
				{ID: "secret-value", State: "done", Progress: -1, Msg: "secret-"},
				{ID: "value", State: "done", Progress: -1},
			} {
				if _, err := e.programStatus.Write(report.Encode()); err != nil {
					t.Fatal(err)
				}
			}
			restore()
			root.End()

			spans := recorder.Ended()
			assertProgramStatusEvents(t, findOnlySpan(t, spans, "agent pre-command hook"), [][3]string{
				{"hook/agent/pre-command", "working", "Running agent pre-command hook"},
				{"build/check", "blocked", "Need [REDACTED]"},
				{"hook/agent/pre-command", "done", ""},
			})
			assertProgramStatusEvents(t, findOnlySpan(t, spans, "default command hook"), [][3]string{
				{"build/test", "error", "Test failed: [REDACTED] [REDACTED] [REDACTED]"},
			})
			assertProgramStatusEvents(t, findOnlySpan(t, spans, "agent post-command hook"), [][3]string{
				{"hook/agent/post-command", "working", "Running agent post-command hook"},
				{"build/cleanup", "done", "Cleaned up"},
				{"hook/agent/post-command", "done", ""},
			})
			wantFinish := [3]string{"command", "done", ""}
			wantStatus := codes.Unset
			if exitCode != 0 {
				wantFinish = [3]string{"command", "error", "The command exited with status 7"}
				wantStatus = codes.Error
			}
			commandSpan := findOnlySpan(t, spans, "command")
			assertProgramStatusEvents(t, commandSpan, [][3]string{{"command", "working", "Running the command"}, wantFinish})
			if got := commandSpan.Status().Code; got != wantStatus {
				t.Errorf("command span status = %v, want %v", got, wantStatus)
			}
			assertProgramStatusEvents(t, findOnlySpan(t, spans, "job"), [][3]string{
				{"hook/agent/pre-command", "clear", ""},
				{"hook/agent/post-command", "clear", ""},
				{"", "idle", "Finished [REDACTED]"},
				{"secret-", "working", "value"},
				{"[REDACTED]", "done", "secret-"},
				{"value", "done", ""},
			})
		})
	}
}

func assertProgramStatusEvents(t *testing.T, span sdktrace.ReadOnlySpan, want [][3]string) {
	t.Helper()
	var got [][3]string
	for _, event := range span.Events() {
		if event.Name != "buildkite.program_status" {
			continue
		}
		attrs := make(map[string]string)
		for _, a := range event.Attributes {
			attrs[string(a.Key)] = a.Value.AsString()
		}
		for _, key := range []string{"buildkite.program_status.id", "buildkite.program_status.state", "buildkite.program_status.message"} {
			if _, ok := attrs[key]; !ok {
				t.Errorf("event missing attribute %q", key)
			}
		}
		got = append(got, [3]string{attrs["buildkite.program_status.id"], attrs["buildkite.program_status.state"], attrs["buildkite.program_status.message"]})
		if event.Time.Before(span.StartTime()) || event.Time.After(span.EndTime()) {
			t.Errorf("event timestamp %v outside span lifetime", event.Time)
		}
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("%s program status events (-want +got):\n%s", span.Name(), diff)
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
