package shell_test

import (
	"bytes"
	"fmt"
	"io"
	"runtime"
	"testing"

	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/google/go-cmp/cmp"
)

const ESC = "\x1b"

func TestAnsiLogger(t *testing.T) {
	got := &bytes.Buffer{}
	l := shell.NewWriterLogger(got, false, nil)

	l.Headerf("Testing header: %q", "llamas")
	l.Printf("Testing print: %q", "llamas")
	l.Commentf("Testing comment: %q", "llamas")
	l.Errorf("Testing error: %q", "llamas")
	l.Warningf("Testing warning: %q", "llamas")
	l.Promptf("Testing prompt: %q", "llamas")

	want := &bytes.Buffer{}

	//nolint:errcheck // Writes to bytes.Buffer never error.
	func() {
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
	}()

	if diff := cmp.Diff(got.String(), want.String()); diff != "" {
		t.Fatalf("shell.WriterLogger output buffer diff (-got +want):\n%s", diff)
	}
}

func TestAnsiWithColorsLogger(t *testing.T) {
	got := &bytes.Buffer{}
	l := shell.NewWriterLogger(got, true, nil)

	l.Headerf("Testing header: %q", "llamas")
	l.Printf("Testing print: %q", "llamas")
	l.Commentf("Testing comment: %q", "llamas")
	l.Errorf("Testing error: %q", "llamas")
	l.Warningf("Testing warning: %q", "llamas")
	l.Promptf("Testing prompt: %q", "llamas")

	want := &bytes.Buffer{}

	//nolint:errcheck // Writes to bytes.Buffer never error.
	func() {
		fmt.Fprintln(want, `~~~ Testing header: "llamas"`)
		fmt.Fprintln(want, `Testing print: "llamas"`)
		fmt.Fprintln(want, ESC+`[90m# Testing comment: "llamas"`+ESC+`[0m`)
		fmt.Fprintln(want, ESC+`[31m🚨 Error: Testing error: "llamas"`+ESC+`[0m`)
		fmt.Fprintln(want, "^^^ +++")
		fmt.Fprintln(want, ESC+`[33m⚠️ Warning: Testing warning: "llamas"`+ESC+`[0m`)
		fmt.Fprintln(want, "^^^ +++")

		if runtime.GOOS == "windows" {
			fmt.Fprintln(want, ESC+"[90m>"+ESC+`[0m Testing prompt: "llamas"`)
		} else {
			fmt.Fprintln(want, ESC+"[90m$"+ESC+`[0m Testing prompt: "llamas"`)
		}
	}()

	if diff := cmp.Diff(got.String(), want.String()); diff != "" {
		t.Fatalf("shell.WriterLogger output buffer diff (-got +want):\n%s", diff)
	}
}

func TestLoggerStreamer(t *testing.T) {
	got := &bytes.Buffer{}
	l := shell.NewWriterLogger(got, false, nil)

	streamer := shell.NewLoggerStreamer(l)
	streamer.Prefix = "TEST>"

	//nolint:errcheck // Writes to the underlying bytes.Buffer never error.
	func() {
		fmt.Fprintf(streamer, "#")
		fmt.Fprintln(streamer, " Rest of the line")
		fmt.Fprintf(streamer, "#")
		fmt.Fprintln(streamer, " And another")
		fmt.Fprint(streamer, "# No line end")

		if err := streamer.Close(); err != nil {
			t.Errorf("streamer.Close() = %v", err)
		}
	}()

	want := &bytes.Buffer{}

	//nolint:errcheck // Writes to bytes.Buffer never error.
	func() {
		fmt.Fprintln(want, "TEST># Rest of the line")
		fmt.Fprintln(want, "TEST># And another")
		fmt.Fprintln(want, "TEST># No line end")
	}()

	if diff := cmp.Diff(got.String(), want.String()); diff != "" {
		t.Fatalf("shell.WriterLogger output buffer diff (-got +want):\n%s", diff)
	}
}

func BenchmarkDoubleFmt(b *testing.B) {
	//nolint:errcheck // Writes to io.Discard never error.
	logf := func(format string, v ...any) {
		fmt.Fprintf(io.Discard, "%s", fmt.Sprintf(format, v...))
		fmt.Fprintln(io.Discard)
	}
	for b.Loop() {
		logf("asdfghjkl %s %d %t", "hi", 42, true)
	}
}

func BenchmarkFmtConcat(b *testing.B) {
	//nolint:errcheck // Writes to io.Discard never error.
	logf := func(format string, v ...any) {
		fmt.Fprintf(io.Discard, format+"\n", v...)
	}
	for b.Loop() {
		logf("asdfghjkl %s %d %t", "hi", 42, true)
	}
}
