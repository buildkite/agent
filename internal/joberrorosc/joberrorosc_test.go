package joberrorosc_test

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/internal/joberrorosc"
	"github.com/buildkite/agent/v4/jobapi"
)

func TestStandardErrorsAreDecodedRedactedAndValidated(t *testing.T) {
	t.Parallel()
	for _, size := range []int{1, 17, 4096} {
		var output bytes.Buffer
		var got []*jobapi.CapturedError
		needles := []string{"initial-secret"}
		w := joberrorosc.NewWriter(&output, func() []string { return needles }, func(e *jobapi.CapturedError) { got = append(got, e) })
		// Write the OSC bytes directly, as a shell/customer can, rather than
		// deriving the expected message from the library's text sanitization.
		write := func(message string) {
			t.Helper()
			input := "before\n\x1b]7501;state=error:msg=" + base64.StdEncoding.EncodeToString([]byte(message)) + "\x1b\\after\n"
			for remaining := input; len(remaining) > 0; {
				n := min(size, len(remaining))
				if _, err := w.Write([]byte(remaining[:n])); err != nil {
					t.Fatal(err)
				}
				remaining = remaining[n:]
			}
			if !strings.HasSuffix(output.String(), input) {
				t.Fatalf("write size %d changed the raw log", size)
			}
		}
		write("Failed with initial-secret")
		needles = append(needles, "runtime-secret")
		write("Failed with runtime-secret at https://user:password@example.com/path?signature=secret")
		write(strings.Repeat("a", 995) + "initial-secret") // redact before the API's 1000-character cut
		for i, want := range []string{
			"Failed with [REDACTED]",
			"Failed with [REDACTED] at https://xxxxx@example.com/path?[REDACTED]",
			strings.Repeat("a", 988) + "…[truncated]",
		} {
			if len(got) != 3 || got[i].Message != want || got[i].Code != joberrorosc.Code || got[i].Timestamp == nil || got[i].IdempotencyKey == "" {
				t.Fatalf("write size %d, report %d: got %+v, want message %q", size, i, got, want)
			}
		}
		before := len(got)
		for _, invalid := range []string{
			"\x1b]7501;state=working:msg=ZmFpbGVk\x07",
			"\x1b]7501;state=error:msg=!!!\x07",
			"\x1b]7501;state=error:msg=IA==\x07", // blank message
			"\x1b]7501;state=error:msg=YQBi\x07", // NUL
			"\x1b]7501;state=error:msg=YQpi\x07", // newline (not standard OSC text)
			"\x1b]7501;state=error:msg=/w==\x07", // invalid UTF-8
			"\x1b]7501;state=error:msg=" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 2049))) + "\x07",
			"\x1b]7501;state=error:msg=ZmFpbGVk", // incomplete
		} {
			if _, err := w.Write([]byte(invalid)); err != nil {
				t.Fatal(err)
			}
		}
		if len(got) != before {
			t.Fatalf("invalid or non-error OSC created API reports: %+v", got[before:])
		}
		// A damaged/incomplete report must not swallow the next valid error.
		write("Failed after incomplete report")
		if len(got) != before+1 || got[before].Message != "Failed after incomplete report" {
			t.Fatalf("did not recover after invalid OSC: %+v", got[before:])
		}
	}
}
