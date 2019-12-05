package process_test

import (
	"bytes"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/buildkite/agent/v3/process"
	"github.com/stretchr/testify/assert"
)

func TestPrefixer(t *testing.T) {
	for _, tc := range []struct {
		input, expected string
	}{
		{"alpacas\nllamas\n", "#1: alpacas\n#2: llamas\n#3: "},
		{"blah\x1b[Kbler\x1bgh", "#1: blah\x1b[K#2: bler\x1bgh"},
		{"blah\x1b[2Kblergh", "#1: blah\x1b[2K#2: blergh"},
		{"blah\x1b[1B\x1b[1A\x1b[2Kblergh", "#1: blah\x1b[1B\x1b[1A\x1b[2K#2: blergh"},
	} {
		tc := tc
		t.Run("", func(tt *testing.T) {
			tt.Parallel()

			var lineCounter int32
			var out = &bytes.Buffer{}

			pw := process.NewPrefixer(out, func() string {
				lineNumber := atomic.AddInt32(&lineCounter, 1)
				return fmt.Sprintf("#%d: ", lineNumber)
			})

			n, err := pw.Write([]byte(tc.input))
			if err != nil {
				t.Fatal(err)
			}

			if expected := len(tc.input); n != expected {
				tt.Fatalf("Short write: %d vs expected %d", n, expected)
			}

			assert.Equal(tt, tc.expected, out.String())
		})
	}
}
