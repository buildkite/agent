package process_test

import (
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/process"
	"github.com/buildkite/agent/v4/logger"
	"github.com/google/go-cmp/cmp"
)

const longTestOutput = `+++ My header
llamas
and more llamas
a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line a very long line
and some alpacas
`

func TestScanLines(t *testing.T) {
	var lineCounter int32
	var lines []string

	pr, pw := io.Pipe()

	go func() {
		defer func() {
			if err := pw.Close(); err != nil {
				t.Errorf("pw.Close() = %v", err)
			}
		}()
		for line := range strings.SplitSeq(strings.TrimSuffix(longTestOutput, "\n"), "\n") {
			if _, err := fmt.Fprintf(pw, "%s\n", line); err != nil {
				t.Errorf("fmt.Fprintf(pw, %q) error = %v", line, err)
				return
			}
			time.Sleep(time.Millisecond * 10)
		}
	}()

	scanner := process.NewScanner(logger.Discard)

	scanFunc := func(l string) {
		lineNumber := atomic.AddInt32(&lineCounter, 1)
		lines = append(lines, fmt.Sprintf("#%d: chars %d", lineNumber, len(l)))
	}

	if err := scanner.ScanLines(pr, scanFunc); err != nil {
		t.Fatalf("scanner.ScanLines(pr, scanFunc) = %v", err)
	}

	wantLines := []string{
		"#1: chars 13",
		"#2: chars 6",
		"#3: chars 15",
		"#4: chars 237",
		"#5: chars 16",
	}

	if diff := cmp.Diff(lines, wantLines); diff != "" {
		t.Errorf("lines diff (-got +want):\n%s", diff)
	}
}
