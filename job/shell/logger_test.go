package shell_test

import (
	"bytes"
	"fmt"
	"runtime"
	"testing"

	"github.com/buildkite/agent/v3/job/shell"
	"github.com/google/go-cmp/cmp"
)

func TestAnsiLogger(t *testing.T) {
	got := &bytes.Buffer{}
	l := shell.WriterLogger{Writer: got, Ansi: false}

	l.Headerf("Testing header: %q", "llamas")
	l.Printf("Testing print: %q", "llamas")
	l.Commentf("Testing comment: %q", "llamas")
	l.Errorf("Testing error: %q", "llamas")
	l.Warningf("Testing warning: %q", "llamas")
	l.Promptf("Testing prompt: %q", "llamas")

	want := &bytes.Buffer{}

	fmt.Fprintln(want, `~~~ Testing header: "llamas"`)
	fmt.Fprintln(want, `Testing print: "llamas"`)
	fmt.Fprintln(want, `# Testing comment: "llamas"`)
	fmt.Fprintln(want, `🚨 Error: Testing error: "llamas"`)
	fmt.Fprintln(want, "^^^ +++")
	fmt.Fprintln(want, `⚠️ Warning: Testing warning: "llamas"`)
	fmt.Fprintln(want, "^^^ +++")

	if runtime.GOOS == "windows" {
		fmt.Fprintln(want, `> Testing prompt: "llamas"`)
	} else {
		fmt.Fprintln(want, `$ Testing prompt: "llamas"`)
	}

	if diff := cmp.Diff(got.String(), want.String()); diff != "" {
		t.Fatalf("shell.WriterLogger output buffer diff (-got +want):\n%s", diff)
	}
}

func TestLoggerStreamer(t *testing.T) {
	got := &bytes.Buffer{}
	l := &shell.WriterLogger{Writer: got, Ansi: false}

	streamer := shell.NewLoggerStreamer(l)
	streamer.Prefix = "TEST>"

	fmt.Fprintf(streamer, "#")
	fmt.Fprintln(streamer, " Rest of the line")
	fmt.Fprintf(streamer, "#")
	fmt.Fprintln(streamer, " And another")
	fmt.Fprint(streamer, "# No line end")

	if err := streamer.Close(); err != nil {
		t.Errorf("streamer.Close() = %v", err)
	}

	want := &bytes.Buffer{}

	fmt.Fprintln(want, "TEST># Rest of the line")
	fmt.Fprintln(want, "TEST># And another")
	fmt.Fprintln(want, "TEST># No line end")

	if diff := cmp.Diff(got.String(), want.String()); diff != "" {
		t.Fatalf("shell.WriterLogger output buffer diff (-got +want):\n%s", diff)
	}
}
