package process_test

import (
	"bytes"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/buildkite/agent/v3/process"
	"github.com/google/go-cmp/cmp"
)

func TestPrefixer(t *testing.T) {
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
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			var lineCounter int32
			out := &bytes.Buffer{}

			pw := process.NewPrefixer(out, func() string {
				lineNumber := atomic.AddInt32(&lineCounter, 1)
				return fmt.Sprintf("#%d: ", lineNumber)
			})

			n, err := pw.Write([]byte(tc.input))
			if err != nil {
				t.Fatalf("pw.Write([]byte(%q)) error = %v", tc.input, err)
			}
			if err := pw.Flush(); err != nil {
				t.Fatalf("pw.Flush() = %v", err)
			}

			if got, want := n, len(tc.input); got != want {
				t.Errorf("pw.Write([]byte(%q)) length = %d, want %d", tc.input, got, want)
			}

			if diff := cmp.Diff(out.String(), tc.want); diff != "" {
				t.Errorf("prefixer output diff (-got +want):\n%s", diff)
			}
		})
	}
}
