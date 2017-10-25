package shell_test

import (
	"bytes"
	"fmt"
	"runtime"
	"testing"

	"github.com/buildkite/agent/bootstrap/shell"
)

func TestAnsiLogger(t *testing.T) {
	b := &bytes.Buffer{}
	l := shell.WriterLogger{Writer: b, Ansi: false}

	l.Headerf("Testing header: %q", "llamas")
	l.Printf("Testing print: %q", "llamas")
	l.Commentf("Testing comment: %q", "llamas")
	l.Errorf("Testing error: %q", "llamas")
	l.Warningf("Testing warning: %q", "llamas")
	l.Promptf("Testing prompt: %q", "llamas")

	expected := &bytes.Buffer{}

	fmt.Fprintln(expected, `~~~ Testing header: "llamas"`)
	fmt.Fprintln(expected, `Testing print: "llamas"`)
	fmt.Fprintln(expected, `# Testing comment: "llamas"`)
	fmt.Fprintln(expected, `🚨 Error: Testing error: "llamas"`)
	fmt.Fprintln(expected, `^^^ +++`)
	fmt.Fprintln(expected, `⚠️ Warning: Testing warning: "llamas"`)
	fmt.Fprintln(expected, `^^^ +++`)

	if runtime.GOOS == "windows" {
		fmt.Fprintln(expected, `> Testing prompt: "llamas"`)
	} else {

		fmt.Fprintln(expected, `$ Testing prompt: "llamas"`)
	}

	actual := b.String()

	if actual != expected.String() {
		t.Fatalf("Expected %q, got %q", expected.String(), actual)
	}
}

func TestLoggerStreamer(t *testing.T) {
	b := &bytes.Buffer{}
	l := &shell.WriterLogger{Writer: b, Ansi: false}

	streamer := shell.NewLoggerStreamer(l)
	streamer.Prefix = "TEST>"

	fmt.Fprintf(streamer, "#")
	fmt.Fprintln(streamer, " Rest of the line")
	fmt.Fprintf(streamer, "#")
	fmt.Fprintln(streamer, " And another")
	fmt.Fprint(streamer, "# No line end")

	if err := streamer.Close(); err != nil {
		t.Fatal(err)
	}

	expected := &bytes.Buffer{}

	fmt.Fprintln(expected, `TEST># Rest of the line`)
	fmt.Fprintln(expected, `TEST># And another`)
	fmt.Fprintln(expected, `TEST># No line end`)

	actual := b.String()

	if actual != expected.String() {
		t.Fatalf("Expected %q, got %q", expected.String(), actual)
	}
}
