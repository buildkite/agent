package process_test

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/process"
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
		for _, line := range strings.Split(strings.TrimSuffix(longTestOutput, "\n"), "\n") {
			fmt.Fprintf(pw, "%s\n", line)
			time.Sleep(time.Millisecond * 10)
		}
		pw.Close()
	}()

	scanner := process.NewScanner(logger.Discard)

	err := scanner.ScanLines(pr, func(l string) {
		lineNumber := atomic.AddInt32(&lineCounter, 1)
		s := fmt.Sprintf("#%d: chars %d", lineNumber, len(l))
		lines = append(lines, s)
	})
	if err != nil {
		t.Fatal(err)
	}

	var expected = []string{
		`#1: chars 13`,
		`#2: chars 6`,
		`#3: chars 15`,
		`#4: chars 237`,
		`#5: chars 16`,
	}

	if !reflect.DeepEqual(expected, lines) {
		t.Fatalf("Lines was unexpected:\nWanted: %v\nGot: %v\n", expected, lines)
	}
}
