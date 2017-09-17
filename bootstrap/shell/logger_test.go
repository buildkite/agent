package shell_test

import (
	"bytes"
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

	expected := `~~~ Testing header: "llamas"` + "\n" +
		`Testing print: "llamas"` + "\n" +
		`# Testing comment: "llamas"` + "\n" +
		`🚨 Error: Testing error: "llamas"` + "\n" +
		`^^^ +++` + "\n" +
		`⚠️ Warning: Testing warning: "llamas"` + "\n" +
		`^^^ +++` + "\n" +
		`$ Testing prompt: "llamas"` + "\n"

	actual := b.String()

	if actual != expected {
		t.Fatalf("Expected %q, got %q", expected, actual)
	}
}
