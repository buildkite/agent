package logger_test

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/logger"
)

func TestConsoleLogger(t *testing.T) {
	b := &bytes.Buffer{}
	exitCode := 0

	printer := logger.NewTextPrinter(b)
	printer.Colors = false

	l := logger.NewConsoleLogger(printer, func(c int) {
		exitCode = c
	})
	l.SetLevel(logger.INFO)

	l.Debugf("Debug %q", "llamas")
	l.Infof("Info %q", "llamas")
	l.Warnf("Warn %q", "llamas")
	l.Errorf("Error %q", "llamas")
	l.Fatalf("Fatal %q", "llamas")

	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")

	if len(lines) != 4 {
		t.Fatalf("bad number of lines, got %d", len(lines))
	}

	if !strings.HasSuffix(lines[0], `Info "llamas"`) {
		t.Fatalf("line 0 bad, got %q", lines[0])
	}

	if !strings.HasSuffix(lines[1], `Warn "llamas"`) {
		t.Fatalf("line 1 bad, got %q", lines[1])
	}

	if !strings.HasSuffix(lines[2], `Error "llamas"`) {
		t.Fatalf("line 2 bad, got %q", lines[2])
	}

	if !strings.HasSuffix(lines[3], `Fatal "llamas"`) {
		t.Fatalf("line 3 bad, got %q", lines[3])
	}

	if exitCode != 1 {
		t.Fatalf("exit code bad, got %d", exitCode)
	}
}

func TestTextPrinter(t *testing.T) {
	b := &bytes.Buffer{}

	printer := logger.NewTextPrinter(b)
	printer.Colors = false

	printer.Print(logger.INFO, "llamas rock", logger.Fields{logger.StringField("key", "val")})

	if msg := b.String(); !strings.HasSuffix(msg, "llamas rock key=val\n") {
		t.Fatalf("bad message, got %q", msg)
	}
}

type panicWriter struct{}

func (panicWriter) Write([]byte) (int, error) { panic("write failed") }

// A printer whose writer panics must not leave the shared output mutex locked,
// or every later log line, from any printer, blocks forever.
func TestPrintersUnlockWhenWriterPanics(t *testing.T) {
	// Everything runs in the goroutine, so a regression fails at the timeout
	// below instead of hanging the test binary.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, printer := range []logger.Printer{
			logger.NewTextPrinter(panicWriter{}),
			logger.NewJSONPrinter(panicWriter{}),
		} {
			func() {
				defer func() {
					if recover() == nil {
						t.Errorf("%T.Print didn't panic with a panicking writer", printer)
					}
				}()
				printer.Print(logger.INFO, "llamas", nil)
			}()
		}
		logger.NewTextPrinter(io.Discard).Print(logger.INFO, "alpacas", nil)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Print blocked after a writer panicked; the output mutex was left locked")
	}
}

func TestJSONPrinter(t *testing.T) {
	b := &bytes.Buffer{}

	printer := logger.NewJSONPrinter(b)
	printer.Print(logger.INFO, "llamas rock", logger.Fields{logger.StringField("key", "val")})

	var results map[string]any
	err := json.Unmarshal(b.Bytes(), &results)
	if err != nil {
		t.Fatalf("bad json: %v", err)
	}

	if val, ok := results["key"]; !ok || val != "val" {
		t.Fatalf("bad key, got %v", val)
	}

	if val, ok := results["msg"]; !ok || val != "llamas rock" {
		t.Fatalf("bad msg, got %v", val)
	}

	if val, ok := results["ts"]; !ok || val == "" {
		t.Fatalf("bad ts, got %v", val)
	}

	if val, ok := results["level"]; !ok || val != "INFO" {
		t.Fatalf("bad level, got %v", val)
	}
}

func TestJSONPrinterSpecialCharacters(t *testing.T) {
	b := &bytes.Buffer{}

	printer := logger.NewJSONPrinter(b)
	printer.Print(logger.INFO, "\x1b", logger.Fields{logger.StringField("key", "val")})

	var results map[string]any
	err := json.Unmarshal(b.Bytes(), &results)
	if err != nil {
		t.Fatalf("bad json: %v", err)
	}
}
