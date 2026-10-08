package job

import (
	"io"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/shell"
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
		{name: "counts characters", writes: []string{"ééééé"}, limit: 3, want: "ééé"},
		{name: "URL credentials are masked before the cut", writes: []string{"fatal: https://user:pass-word@host/repo"}, limit: 20, want: "ps://xxxxx@host/repo"},
		{name: "URL queries are masked before the cut", writes: []string{"GET https://host/a?sig=abcdefghij failed"}, limit: 24, want: "host/a?[REDACTED] failed"},
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

func TestRecentOutputRedaction(t *testing.T) {
	t.Parallel()

	e := New(ExecutorConfig{})
	e.setupRedactors(shell.DiscardLogger, env.New(), io.Discard, io.Discard)
	write := func(s string) {
		t.Helper()
		if _, err := io.WriteString(e.outputTailRedactor, s); err != nil {
			t.Fatal(err)
		}
	}

	// A Buildkite token is redacted even if it was never registered, so
	// cutting through it cannot leave a fragment without its prefix.
	write("token bkua_" + strings.Repeat("a", 40) + " rejected")
	if err := e.outputTailRedactor.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := e.outputTail.tail(len("aaaa rejected")); strings.Contains(got, "aaaa") {
		t.Errorf("tail = %q, want the token redacted before the cut", got)
	}

	// Output held back in case it starts a token is not joined to the next
	// hook's output.
	write("ends with bkua_")
	e.resetRecentOutput()
	write("next\n")
	if err := e.outputTailRedactor.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := e.outputTail.tail(100); got != "next" {
		t.Errorf("tail after reset = %q, want %q", got, "next")
	}
}
