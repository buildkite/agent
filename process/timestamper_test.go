package process_test

import (
	"bytes"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/process"
	"github.com/google/go-cmp/cmp"
)

func TestTimestamper(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input, want string
	}{
		{
			input: "alpacas\nllamas\n",
			want:  "#1: alpacas\n#2: llamas\n",
		},
		{
			input: "blah\x1b[Kbler\x1bgh",
			want:  "#1: blah\x1b[K#2: bler\x1bgh",
		},
		{
			input: "blah\x1b[2Kblergh",
			want:  "#1: blah\x1b[2K#2: blergh",
		},
		{
			input: "blah\x1b[1B\x1b[1A\x1b[2Kblergh",
			want:  "#1: blah\x1b[1B\x1b[1A\x1b[2K#2: blergh",
		},
		{
			input: "foo\n\x1b[1B and then some time later square-bracket K [Kllama",
			want:  "#1: foo\n#2: \x1b[1B and then some time later square-bracket K [Kllama",
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			var lineCounter int32
			out := &bytes.Buffer{}

			pw := process.NewTimestamper(out, func(time.Time) string {
				lineNumber := atomic.AddInt32(&lineCounter, 1)
				return fmt.Sprintf("#%d: ", lineNumber)
			}, 1*time.Second)

			n, err := pw.Write([]byte(tc.input))
			if err != nil {
				t.Fatalf("pw.Write([]byte(%q)) error = %v", tc.input, err)
			}

			if got, want := n, len(tc.input); got != want {
				t.Errorf("pw.Write([]byte(%q)) length = %d, want %d", tc.input, got, want)
			}

			if diff := cmp.Diff(out.String(), tc.want); diff != "" {
				t.Errorf("timestamper output diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestTimestamper_WithTimeout(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	pw := process.NewTimestamper(out, func(time.Time) string {
		return "..."
	}, 10*time.Millisecond)

	if _, err := pw.Write([]byte("I see you ")); err != nil {
		t.Fatalf("pw.Write(`I see you `) error = %v", err)
	}

	// Another write on the same line immediately should _not_ trigger a new
	// timestamp
	if _, err := pw.Write([]byte("shiver with antici\x1b[")); err != nil {
		t.Fatalf("pw.Write(`shiver with antici\x1b[`) error = %v", err)
	}

	// A write on the same line some time later should trigger a new timestamp
	// but not in the middle of an ANSI sequence
	time.Sleep(100 * time.Millisecond)
	if _, err := pw.Write([]byte("1mpation")); err != nil {
		t.Fatalf("pw.Write(`1mpation`) error = %v", err)
	}

	want := "...I see you shiver with antici\x1b[1m...pation"
	if diff := cmp.Diff(out.String(), want); diff != "" {
		t.Errorf("timestamper output diff (-got +want):\n%s", diff)
	}
}
