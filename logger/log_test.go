package logger

import (
	"bytes"
	"strings"
	"testing"
)

func TestLevelLogger(t *testing.T) {
	b := &bytes.Buffer{}
	l := NewLogger()
	l.Level = INFO
	l.Colors = false
	l.Writer = b

	l.Debug("Debug %q", "llamas")
	l.Info("Info %q", "llamas")
	l.Warn("Warn %q", "llamas")
	l.Error("Error %q", "llamas")

	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")

	if len(lines) != 3 {
		t.Fatalf("bad number of lines, got %d", len(lines))
	}

	if !strings.HasSuffix(lines[0], `Info "llamas"`) {
		t.Fatalf("line 0 bad, got %q", lines[0])
	}

	if !strings.HasSuffix(lines[1], `Warn "llamas"`) {
		t.Fatalf("line 1 bad, got %q", lines[1])
	}

	if !strings.HasSuffix(lines[2], `Error "llamas"`) {
		t.Fatalf("line 0 bad, got %q", lines[2])
	}
}
