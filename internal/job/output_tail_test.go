package job

import (
	"io"
	"strings"
	"testing"
)

func TestOutputTail(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		writes []string
		limit  int
		want   string
	}{
		{name: "empty", limit: 100, want: ""},
		{name: "whole output", writes: []string{"line one\n", "line two\n"}, limit: 100, want: "line one\nline two"},
		{name: "starts at a line boundary", writes: []string{"first line\nsecond line\nthird\n"}, limit: 15, want: "third"},
		{name: "no room", writes: []string{"output\n"}, limit: 0, want: ""},
		{name: "terminal formatting", writes: []string{"\x1b[31mFAIL\x1b[0m: TestThing\r\n"}, limit: 100, want: "FAIL: TestThing"},
		{name: "long output keeps the end", writes: []string{strings.Repeat("early\n", 1000), "the last line\n"}, limit: 20, want: "early\nthe last line"},
		{name: "split multibyte character", writes: []string{"ééééé"}, limit: 5, want: "éé"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var tail outputTail
			for _, w := range test.writes {
				if n, err := io.WriteString(&tail, w); err != nil || n != len(w) {
					t.Fatalf("Write = %d, %v", n, err)
				}
			}
			if got := tail.tail(test.limit); got != test.want {
				t.Errorf("tail(%d) = %q, want %q", test.limit, got, test.want)
			}
		})
	}
}

func TestOutputTailReset(t *testing.T) {
	t.Parallel()
	var tail outputTail
	_, _ = io.WriteString(&tail, "from an earlier hook\n")
	tail.reset()
	_, _ = io.WriteString(&tail, "from this hook\n")
	if got := tail.tail(100); got != "from this hook" {
		t.Errorf("tail after reset = %q", got)
	}
}
