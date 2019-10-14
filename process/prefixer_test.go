package process_test

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/buildkite/agent/process"
)

func TestPrefixer(t *testing.T) {
	var lineCounter int32
	var input string = "blah\nblergh\n\nnope\n"
	var out = &bytes.Buffer{}

	pw := process.NewPrefixer(out, func() string {
		lineNumber := atomic.AddInt32(&lineCounter, 1)
		return fmt.Sprintf("#%d: ", lineNumber)
	})

	n, err := pw.Write([]byte(input))
	if err != nil {
		t.Fatal(err)
	}

	if expected := len(input); n != expected {
		t.Fatalf("Short write: %d vs expected %d", n, expected)
	}

	lines := strings.Split(out.String(), "\n")

	var expected = []string{
		`#1: blah`,
		`#2: blergh`,
		`#3: `,
		`#4: nope`,
		`#5: `,
	}

	if !reflect.DeepEqual(expected, lines) {
		t.Fatalf("Lines was unexpected:\nWanted: %v\nGot: %v\n", expected, lines)
	}
}
