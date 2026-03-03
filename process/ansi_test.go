package process

import (
	"os"
	"testing"
)

func TestANSIParser(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  bool
	}{
		{
			input: "unadorned input",
			want:  false,
		},
		{
			input: "here's a CSI: \x1b[K and some regular text",
			want:  false,
		},
		{
			input: "some text followed by an incomplete CSI: \x1b[",
			want:  true,
		},
		{
			input: "a single escape byte: \x1b",
			want:  true,
		},
		{
			input: "complete BK timestamp: \x1b_bk;t=12345\x07",
			want:  false,
		},
		{
			input: "complete SOS: \x1bXasdfghjkl\x1b\\",
			want:  false,
		},
		{
			input: "incomplete BK timestamp: \x1b_bk;t=123",
			want:  true,
		},
		{
			input: "incomplete SOS: \x1bXasdfg",
			want:  true,
		},
		{
			input: "PM without ST: \x1b^asdf\x1b/more",
			want:  true,
		},
	}

	for _, test := range tests {
		var p ansiParser
		p.Write([]byte(test.input)) //nolint:errcheck // ansiParser.Write never returns errors
		if got := p.insideCode(); got != test.want {
			t.Errorf("after p.feed(%q...): p.insideCode() = %t, want %t", test.input, got, test.want)
		}
	}
}

func BenchmarkANSIParser(b *testing.B) {
	npm, err := os.ReadFile("fixtures/npm.sh.raw")
	if err != nil {
		b.Fatalf("os.ReadFile(fixtures/npm.sh.raw) error = %v", err)
	}

	for b.Loop() {
		var p ansiParser
		p.Write(npm) //nolint:errcheck // ansiParser.Write never returns errors
	}
}
