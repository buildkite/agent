package replacer_test

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/internal/redact"
	"github.com/buildkite/agent/v4/internal/replacer"
	"github.com/google/go-cmp/cmp"
)

const lipsum = "Lorem ipsum dolor sit amet"

func TestMuxNeedles(t *testing.T) {
	t.Parallel()

	var output strings.Builder
	first := redact.New(&output, []string{"initial-secret"})
	second := redact.New(io.Discard, []string{"initial-secret", "other-secret"})
	mux := replacer.NewMux(first, second)
	if _, err := first.Write([]byte("initial-")); err != nil {
		t.Fatal(err)
	}
	mux.Add("runtime-secret")
	got := mux.Needles()
	slices.Sort(got)
	if diff := cmp.Diff([]string{"initial-secret", "other-secret", "runtime-secret"}, got); diff != "" {
		t.Errorf("Needles() diff (-want +got):\n%s", diff)
	}
	if _, err := first.Write([]byte("secret")); err != nil {
		t.Fatal(err)
	}
	if err := mux.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "[REDACTED]" {
		t.Errorf("output = %q, want [REDACTED]", got)
	}
}

func TestReplacerLoremIpsum(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc    string
		needles []string
		want    string
	}{
		{
			// Redact nothing.
			desc:    "Empty needles",
			needles: nil,
			want:    lipsum,
		},
		{
			// Redact one secret.
			desc:    "ipsum",
			needles: []string{"ipsum"},
			want:    "Lorem [REDACTED] dolor sit amet",
		},
		{
			// Redact two different secrets.
			desc:    "ipsum, amet",
			needles: []string{"ipsum", "amet"},
			want:    "Lorem [REDACTED] dolor sit [REDACTED]",
		},
		{
			// Redact the larger of the secrets.
			desc:    "First secret contains second",
			needles: []string{"ipsum dolor", "dolor"},
			want:    "Lorem [REDACTED] sit amet",
		},
		{
			// Redact the larger of the secrets.
			desc:    "Second secret contains first",
			needles: []string{"ipsum", "ipsum dolor"},
			want:    "Lorem [REDACTED] sit amet",
		},
		{
			// The second secret starts matching while the first is matching,
			// and ultimately matches too. Redact as one.
			desc:    "Overlapping secrets",
			needles: []string{"ipsum dolor", "dolor sit"},
			want:    "Lorem [REDACTED] amet",
		},
		{
			// The second secret starts matching while the first is matching,
			// but ultimately doesn't match.
			// The third secret starts matching while the second is matching,
			// and ultimately DOES match.
			// But the first and third do not overlap at all.
			// Redact two separate times (first and third secrets).
			desc:    "Overlapping secrets 2",
			needles: []string{"ipsum dolor", "dolor sEt", "sit amet"},
			want:    "Lorem [REDACTED] [REDACTED]",
		},
		{
			// The first secret doesn't match, but spends most of the string
			// matching, including the other secrets. The second and third
			// secrets match.
			desc:    "Overlapping secrets 3",
			needles: []string{"Lorem ipsum dolor sEt", "ipsum", "dolor"},
			want:    "Lorem [REDACTED] [REDACTED] sit amet",
		},
		{
			// A more extreme variation of the above
			desc:    "Vowels",
			needles: []string{"a", "e", "i", "o", "u", "Lorem ipsum dolor sit am3t"},
			want:    "L[REDACTED]r[REDACTED]m [REDACTED]ps[REDACTED]m d[REDACTED]l[REDACTED]r s[REDACTED]t [REDACTED]m[REDACTED]t",
		},
		{
			// Tower of nested secrets.
			desc:    "Tower of secrets",
			needles: []string{"do", " dol", "m dolo", "um dolor", "sum dolor ", "psum dolor s", "ipsum dolor si"},
			want:    "Lorem [REDACTED]t amet",
		},
	}

	for _, test := range tests {
		// Write input in a single Write call
		t.Run("One write;"+test.desc, func(t *testing.T) {
			t.Parallel()

			var buf strings.Builder
			replacer := replacer.New(&buf, test.needles, redact.Redacted)
			if _, err := fmt.Fprint(replacer, lipsum); err != nil {
				t.Errorf("fmt.Fprint(replacer, lipsum) error = %v", err)
			}
			if err := replacer.Flush(); err != nil {
				t.Errorf("replacer.Flush() = %v", err)
			}

			if got, want := buf.String(), test.want; got != want {
				t.Errorf("post-redaction(needles = %q) buf.String() = %q, want %q", test.needles, got, want)
			}
		})

		// "Slow Loris": write one byte at a time
		t.Run("Many writes;"+test.desc, func(t *testing.T) {
			t.Parallel()

			var buf strings.Builder
			replacer := replacer.New(&buf, test.needles, redact.Redacted)
			for _, c := range []byte(lipsum) {
				if _, err := replacer.Write([]byte{c}); err != nil {
					t.Errorf("replacer.Write([]byte{%d}) error = %v", c, err)
				}
			}
			if err := replacer.Flush(); err != nil {
				t.Errorf("replacer.Flush() = %v", err)
			}
			if got, want := buf.String(), test.want; got != want {
				t.Errorf("post-redaction(needles = %q) buf.String() = %q, want %q", test.needles, got, want)
			}
		})
	}
}

func TestReplacerWriteBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc    string
		inputs  []string
		needles []string
		want    string
	}{
		{
			desc:    "Break stream mid-secret",
			inputs:  []string{"Lorem ip", "sum dolor sit amet"},
			needles: []string{"ipsum"},
			want:    "Lorem [REDACTED] dolor sit amet",
		},
		{
			desc: "Break stream mid-secret with pending redaction",
			// "um do" is marked as a redacted range, but because "ipsum dolor"
			// is incomplete, [REDACTED] isn't written out yet. Later,
			// "ipsum dolor" finishes matching, and [REDACTED] is written out.
			inputs:  []string{"Lorem ipsum dol", "or sit amet"},
			needles: []string{"ipsum dolor", "um do"},
			want:    "Lorem [REDACTED] sit amet",
		},
		{
			desc:    "Break stream mid-secrets with multiple pending redactions",
			inputs:  []string{"Lorem ipsum dol", "or sit amet"},
			needles: []string{"ipsum dolor", "sum", "do", "dolor"},
			want:    "Lorem [REDACTED] sit amet",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()
			var buf strings.Builder

			replacer := replacer.New(&buf, test.needles, redact.Redacted)

			for _, input := range test.inputs {
				if _, err := fmt.Fprint(replacer, input); err != nil {
					t.Errorf("fmt.Fprint(replacer, %q) error = %v", input, err)
				}
			}
			if err := replacer.Flush(); err != nil {
				t.Errorf("replacer.Flush() = %v", err)
			}
			if got, want := buf.String(), test.want; got != want {
				t.Errorf("post-redaction(needles = %q) buf.String() = %q, want %q", test.needles, got, want)
			}
		})
	}
}

func TestReplacerResetMidStream(t *testing.T) {
	t.Parallel()

	var buf strings.Builder
	replacer := replacer.New(&buf, []string{"secret1111"}, redact.Redacted)

	// start writing to the stream (no trailing newline, to be extra tricky)
	input := "redact secret1111 but don't redact secret2222 until"
	if _, err := replacer.Write([]byte(input)); err != nil {
		t.Errorf("replacer.Write(%q) error = %v", input, err)
	}

	// update the replacer with a new secret
	// replacer.Flush() // manual flush is NOT necessary before Reset
	replacer.Reset([]string{"secret1111", "secret2222"})

	// finish writing
	input = " after secret2222 is added\n"
	if _, err := replacer.Write([]byte(input)); err != nil {
		t.Errorf("replacer.Write(%q) error = %v", input, err)
	}
	if err := replacer.Flush(); err != nil {
		t.Errorf("replacer.Flush() = %v", err)
	}
	if got, want := buf.String(), "redact [REDACTED] but don't redact secret2222 until after [REDACTED] is added\n"; got != want {
		t.Errorf("post-redaction buf.String() = %q, want %q", got, want)
	}
}

func TestReplacerMultibyte(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	replacer := replacer.New(&buf, []string{"ÿ"}, redact.Redacted)

	input := "fooÿbar"
	if _, err := replacer.Write([]byte(input)); err != nil {
		t.Errorf("replacer.Write(%q) error = %v", input, err)
	}
	if err := replacer.Flush(); err != nil {
		t.Errorf("replacer.Flush() = %v", err)
	}
	if got, want := buf.String(), "foo[REDACTED]bar"; got != want {
		t.Errorf("post-redaction buf.String() = %q, want %q", got, want)
	}
}

func TestReplacerMultiLine(t *testing.T) {
	t.Parallel()

	const secret = "-----BEGIN OPENSSH PRIVATE KEY-----\nasdf\n-----END OPENSSH PRIVATE KEY-----\n"

	tests := []struct {
		name  string
		input []string
		want  string
	}{
		{
			name: "exact",
			input: []string{
				"lalalala\n",
				"-----BEGIN OPENSSH PRIVATE KEY-----\n",
				"asdf\n",
				"-----END OPENSSH PRIVATE KEY-----\n",
				"lalalala\n",
			},
			want: "lalalala\n[REDACTED]\nlalalala\n",
		},
		{
			name: "cr-lf line endings",
			input: []string{
				"lalalala\r\n",
				"-----BEGIN OPENSSH PRIVATE KEY-----\r\n",
				"asdf\r\n",
				"-----END OPENSSH PRIVATE KEY-----\r\n",
				"lalalala\r\n",
			},
			want: "lalalala\r\n[REDACTED]\r\nlalalala\r\n",
		},
		{
			name: "cr-cr-lf line endings",
			// Thanks to some combination of baked-mode PTY and other processing, log
			// output linebreaks often look like \r\r\n, which is annoying both when
			// redacting secrets and when opening them in a text editor.
			input: []string{
				"lalalala\r\r\n",
				"-----BEGIN OPENSSH PRIVATE KEY-----\r\r\n",
				"asdf\r\r\n",
				"-----END OPENSSH PRIVATE KEY-----\r\r\n",
				"lalalala\r\r\n",
			},
			want: "lalalala\r\r\n[REDACTED]\r\r\nlalalala\r\r\n",
		},
		{
			name: "spaces instead of newlines",
			input: []string{
				"lalalala -----BEGIN OPENSSH PRIVATE KEY----- asdf -----END OPENSSH PRIVATE KEY----- lalalala\n",
			},
			want: "lalalala [REDACTED] lalalala\n",
		},
		{
			name: "mixed whitespace garbage",
			input: []string{
				"lalalala\n\n\r\n",
				"-----BEGIN OPENSSH PRIVATE KEY-----\n\n \n\v",
				"asdf\n\t\t\n  \n",
				"-----END OPENSSH PRIVATE KEY-----\n\n\n",
				"lalalala",
			},
			want: "lalalala\n\n\r\n[REDACTED]\n\n\nlalalala",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var buf strings.Builder
			r := replacer.New(&buf, []string{secret}, redact.Redacted)
			for _, line := range test.input {
				if _, err := fmt.Fprint(r, line); err != nil {
					t.Errorf("fmt.Fprint(r, %q) error = %v", line, err)
				}
			}
			if err := r.Flush(); err != nil {
				t.Errorf("r.Flush() = %v", err)
			}

			if diff := cmp.Diff(buf.String(), test.want); diff != "" {
				t.Errorf("post-redaction diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestAddingNeedles(t *testing.T) {
	t.Parallel()

	needles := []string{"secret1111", "secret2222"}
	afterAddExpectedNeedles := []string{"secret1111", "secret2222", "pre-secret3333"}

	var buf strings.Builder
	replacer := replacer.New(&buf, needles, redact.Redacted)
	actualNeedles := replacer.Needles()

	slices.Sort(needles)
	slices.Sort(actualNeedles)
	if diff := cmp.Diff(needles, actualNeedles); diff != "" {
		t.Fatalf("[]string{\"secret1111\", \"secret2222\"} diff (-got +want):\n%s", diff)
	}

	_, err := replacer.Write([]byte("redact secret1111 and secret2222 but not pre-secret3333\n"))
	if err != nil {
		t.Fatalf("replacer.Write([]byte(\"redact secret1111 and secret2222 but not pre-secret3333\\n\")) error = %v, want nil", err)
	}

	replacer.Add("pre-secret3333")
	actualNeedles = replacer.Needles()
	slices.Sort(actualNeedles)
	slices.Sort(afterAddExpectedNeedles)
	if diff := cmp.Diff(afterAddExpectedNeedles, actualNeedles); diff != "" {
		t.Fatalf("[]string{\"secret1111\", \"secret2222\", \"pre-secret3333\"} diff (-got +want):\n%s", diff)
	}

	_, err = replacer.Write([]byte("now redact secret1111, secret2222, and pre-secret3333\n"))
	if err != nil {
		t.Fatalf("replacer.Write([]byte(\"now redact secret1111, secret2222, and pre-secret3333\\n\")) error = %v, want nil", err)
	}
	if err := replacer.Flush(); err != nil {
		t.Fatalf("replacer.Flush() error = %v, want nil", err)
	}

	if got, want := buf.String(), "redact [REDACTED] and [REDACTED] but not pre-secret3333\nnow redact [REDACTED], [REDACTED], and [REDACTED]\n"; got != want {
		t.Fatalf("buf.String() = %q, want %q", got, want)
	}
}

// testPrefix is a small prefix pattern for exercising the prefix-matching
// mode: "tok_" followed by 8 to 16 lowercase letters, digits, '.', '-' or '_'.
var testPrefix = replacer.Prefix{
	Prefix:  "tok_",
	Body:    "abcdefghijklmnopqrstuvwxyz0123456789.-_",
	MinBody: 8,
	MaxBody: 16,
}

func TestReplacerPrefixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc    string
		inputs  []string
		needles []string
		want    string
	}{
		{
			desc:   "Token mid-line",
			inputs: []string{"the token is tok_abcdefgh1234 ok\n"},
			want:   "the token is [REDACTED] ok\n",
		},
		{
			desc:   "Token with dots and dashes in body",
			inputs: []string{"tok_ab.cd-ef.gh\n"},
			want:   "[REDACTED]\n",
		},
		{
			desc:   "Token split across writes inside the prefix",
			inputs: []string{"the token is to", "k_abcdefgh1234 ok\n"},
			want:   "the token is [REDACTED] ok\n",
		},
		{
			desc:   "Token split across writes inside the body",
			inputs: []string{"the token is tok_abcd", "efgh1234 ok\n"},
			want:   "the token is [REDACTED] ok\n",
		},
		{
			desc:   "Token split across writes before the terminator",
			inputs: []string{"the token is tok_abcdefgh1234", " ok\n"},
			want:   "the token is [REDACTED] ok\n",
		},
		{
			desc:   "Token at EOF",
			inputs: []string{"the token is tok_abcdefgh1234"},
			want:   "the token is [REDACTED]",
		},
		{
			desc:   "Token at EOF split across writes",
			inputs: []string{"the token is tok_abc", "defgh1234"},
			want:   "the token is [REDACTED]",
		},
		{
			desc:   "Body exactly MinBody long",
			inputs: []string{"tok_abcdefgh\n"},
			want:   "[REDACTED]\n",
		},
		{
			desc:   "Body exactly MinBody long at EOF",
			inputs: []string{"tok_abcdefgh"},
			want:   "[REDACTED]",
		},
		{
			desc:   "Body one shorter than MinBody",
			inputs: []string{"tok_abcdefg\n"},
			want:   "tok_abcdefg\n",
		},
		{
			desc:   "Body one shorter than MinBody at EOF",
			inputs: []string{"tok_abcdefg"},
			want:   "tok_abcdefg",
		},
		{
			desc:   "Body one shorter than MinBody split across writes",
			inputs: []string{"tok_abc", "defg\n"},
			want:   "tok_abcdefg\n",
		},
		{
			desc:   "Prefix with no body",
			inputs: []string{"tok_ tok_\n"},
			want:   "tok_ tok_\n",
		},
		{
			desc:   "Prefix at EOF",
			inputs: []string{"tok_"},
			want:   "tok_",
		},
		{
			desc:   "Body exactly MaxBody long",
			inputs: []string{"tok_abcdefghijklmnop\n"},
			want:   "[REDACTED]\n",
		},
		{
			desc: "Body longer than MaxBody",
			// The first 16 body bytes are the match; the rest is passed
			// through.
			inputs: []string{"tok_abcdefghijklmnopqrstuvwxyz\n"},
			want:   "[REDACTED]qrstuvwxyz\n",
		},
		{
			desc:   "Body longer than MaxBody at EOF",
			inputs: []string{"tok_abcdefghijklmnopqrstuvwxyz"},
			want:   "[REDACTED]qrstuvwxyz",
		},
		{
			desc: "Truncated token",
			// A token cut off by a line ending (like `ps` truncating a long
			// command line) is still redacted if enough of it is present.
			inputs: []string{"tini -- agent --acquire-job tok_abcdefgh12\nnext line\n"},
			want:   "tini -- agent --acquire-job [REDACTED]\nnext line\n",
		},
		{
			desc:   "Body terminated by non-body ASCII",
			inputs: []string{"\"tok_abcdefgh1234\"\n"},
			want:   "\"[REDACTED]\"\n",
		},
		{
			desc:   "Body terminated by uppercase (not in body set)",
			inputs: []string{"tok_abcdefgh1234XYZ\n"},
			want:   "[REDACTED]XYZ\n",
		},
		{
			desc:   "Two tokens on one line",
			inputs: []string{"tok_abcdefgh1234 tok_zyxwvuts9876\n"},
			want:   "[REDACTED] [REDACTED]\n",
		},
		{
			desc: "Prefix inside a token body",
			// "tok_" is made of body bytes, so a second candidate starts
			// inside the first. Both end at the same place; redact once.
			inputs: []string{"tok_abcdtok_efgh\n"},
			want:   "[REDACTED]\n",
		},
		{
			desc: "Prefix inside a too-short body",
			// The outer candidate is long enough but the inner one isn't.
			inputs: []string{"tok_abcdtok_ef\n"},
			want:   "[REDACTED]\n",
		},
		{
			desc:    "Needle overlapping a token",
			inputs:  []string{"tok_abcdefgh1234 ok\n"},
			needles: []string{"1234 ok"},
			want:    "[REDACTED]\n",
		},
		{
			desc:    "Needle inside a token",
			inputs:  []string{"tok_abcdefgh1234 ok\n"},
			needles: []string{"cdef"},
			want:    "[REDACTED] ok\n",
		},
		{
			desc:    "Needle inside a too-short token",
			inputs:  []string{"tok_abcdef ok\n"},
			needles: []string{"cdef"},
			want:    "tok_ab[REDACTED] ok\n",
		},
		{
			desc:    "Needle adjacent to a token",
			inputs:  []string{"key=tok_abcdefgh1234\n"},
			needles: []string{"key="},
			want:    "[REDACTED][REDACTED]\n",
		},
		{
			desc:    "Needle that is a token prefix",
			inputs:  []string{"tok_abcdefgh1234\n"},
			needles: []string{"tok_abcd"},
			want:    "[REDACTED]\n",
		},
	}

	for _, test := range tests {
		t.Run("Given writes;"+test.desc, func(t *testing.T) {
			t.Parallel()

			var buf strings.Builder
			r := replacer.New(&buf, test.needles, redact.Redacted)
			r.AddPrefixes(testPrefix)
			for _, input := range test.inputs {
				if _, err := fmt.Fprint(r, input); err != nil {
					t.Errorf("fmt.Fprint(r, %q) error = %v", input, err)
				}
			}
			if err := r.Flush(); err != nil {
				t.Errorf("r.Flush() = %v", err)
			}
			if got, want := buf.String(), test.want; got != want {
				t.Errorf("post-redaction(inputs = %q) buf.String() = %q, want %q", test.inputs, got, want)
			}
		})

		// "Slow Loris": write one byte at a time
		t.Run("Many writes;"+test.desc, func(t *testing.T) {
			t.Parallel()

			var buf strings.Builder
			r := replacer.New(&buf, test.needles, redact.Redacted)
			r.AddPrefixes(testPrefix)
			for _, c := range []byte(strings.Join(test.inputs, "")) {
				if _, err := r.Write([]byte{c}); err != nil {
					t.Errorf("r.Write([]byte{%d}) error = %v", c, err)
				}
			}
			if err := r.Flush(); err != nil {
				t.Errorf("r.Flush() = %v", err)
			}
			if got, want := buf.String(), test.want; got != want {
				t.Errorf("post-redaction(inputs = %q) buf.String() = %q, want %q", test.inputs, got, want)
			}
		})
	}
}

func TestReplacerPrefixesHoldBackIsBounded(t *testing.T) {
	t.Parallel()

	// While a body is matching, output is held back. Once the body reaches
	// MaxBody, the match completes and everything up to it is written, even
	// without a Flush.
	var buf strings.Builder
	r := replacer.New(&buf, nil, redact.Redacted)
	r.AddPrefixes(testPrefix)

	if _, err := fmt.Fprint(r, "before tok_abcdefghijklmnop"); err != nil {
		t.Fatalf("fmt.Fprint(r, ...) error = %v", err)
	}
	if got, want := buf.String(), "before [REDACTED]"; got != want {
		t.Errorf("after writing MaxBody body bytes, buf.String() = %q, want %q", got, want)
	}

	// Anything less than MaxBody is held back until the match resolves.
	buf.Reset()
	if _, err := fmt.Fprint(r, " tok_abcdefghijklmno"); err != nil {
		t.Fatalf("fmt.Fprint(r, ...) error = %v", err)
	}
	if got, want := buf.String(), " "; got != want {
		t.Errorf("while body is matching, buf.String() = %q, want %q", got, want)
	}
	if err := r.Flush(); err != nil {
		t.Fatalf("r.Flush() = %v", err)
	}
	if got, want := buf.String(), " [REDACTED]"; got != want {
		t.Errorf("after Flush, buf.String() = %q, want %q", got, want)
	}
}

func TestReplacerPrefixesSurviveReset(t *testing.T) {
	t.Parallel()

	var buf strings.Builder
	r := replacer.New(&buf, []string{"secret1111"}, redact.Redacted)
	r.AddPrefixes(testPrefix)
	r.Reset([]string{"secret2222"})

	if _, err := fmt.Fprint(r, "secret1111 secret2222 tok_abcdefgh1234\n"); err != nil {
		t.Fatalf("fmt.Fprint(r, ...) error = %v", err)
	}
	if err := r.Flush(); err != nil {
		t.Fatalf("r.Flush() = %v", err)
	}
	if got, want := buf.String(), "secret1111 [REDACTED] [REDACTED]\n"; got != want {
		t.Errorf("buf.String() = %q, want %q", got, want)
	}
}

func TestReplacerAddPrefixesPanicsOnInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc   string
		prefix replacer.Prefix
	}{
		{desc: "empty prefix", prefix: replacer.Prefix{Body: "a", MinBody: 1, MaxBody: 2}},
		{desc: "empty body", prefix: replacer.Prefix{Prefix: "p", MinBody: 1, MaxBody: 2}},
		{desc: "zero MinBody", prefix: replacer.Prefix{Prefix: "p", Body: "a", MinBody: 0, MaxBody: 2}},
		{desc: "MaxBody < MinBody", prefix: replacer.Prefix{Prefix: "p", Body: "a", MinBody: 3, MaxBody: 2}},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Errorf("AddPrefixes(%+v) did not panic", test.prefix)
				}
			}()
			replacer.New(io.Discard, nil, redact.Redacted).AddPrefixes(test.prefix)
		})
	}
}

func BenchmarkReplacerWithTokenPrefixes(b *testing.B) {
	r := replacer.New(io.Discard, bigLipsumSecrets, redact.Redacted)
	r.AddPrefixes(redact.TokenPrefixes()...)
	for b.Loop() {
		if _, err := fmt.Fprintln(r, bigLipsum); err != nil {
			b.Errorf("fmt.Fprintln(r, bigLipsum) error = %v", err)
		}
	}
	if err := r.Flush(); err != nil {
		b.Errorf("replacer.Flush() = %v", err)
	}
}

func BenchmarkReplacer(b *testing.B) {
	r := replacer.New(io.Discard, bigLipsumSecrets, redact.Redacted)
	for b.Loop() {
		if _, err := fmt.Fprintln(r, bigLipsum); err != nil {
			b.Errorf("fmt.Fprintln(r, bigLipsum) error = %v", err)
		}
	}
	if err := r.Flush(); err != nil {
		b.Errorf("replacer.Flush() = %v", err)
	}
}

func FuzzReplacerPrefixes(f *testing.F) {
	f.Add("the token is tok_abcdefgh1234 ok\n", 10)
	f.Add("tok_abcdefgh", 4)
	f.Add("tok_abcdefg\n", 6)
	f.Add("tok_abcdefghijklmnopqrstuvwxyz\n", 20)
	f.Add("tok_abcdtok_efgh\n", 9)
	f.Add("tok_abcdefghijkltok_abcdefgh\n", 0)
	f.Add("tok_ tok_ tok\n", -1)
	f.Fuzz(func(t *testing.T, plaintext string, split int) {
		var sb strings.Builder
		r := replacer.New(&sb, nil, redact.Redacted)
		r.AddPrefixes(testPrefix)

		if split < 0 || split >= len(plaintext) {
			if _, err := fmt.Fprint(r, plaintext); err != nil {
				t.Errorf("fmt.Fprint(r, %q) error = %v", plaintext, err)
			}
		} else {
			if _, err := fmt.Fprint(r, plaintext[:split]); err != nil {
				t.Errorf("fmt.Fprint(r, %q) error = %v", plaintext[:split], err)
			}
			if _, err := fmt.Fprint(r, plaintext[split:]); err != nil {
				t.Errorf("fmt.Fprint(r, %q) error = %v", plaintext[split:], err)
			}
		}
		if err := r.Flush(); err != nil {
			t.Errorf("r.Flush() = %v", err)
		}
		got := sb.String()

		// Nothing that looks like a token should survive.
		if loc := tokenLike.FindStringIndex(got); loc != nil {
			t.Errorf("replacer output %q contains token-like %q", got, got[loc[0]:loc[1]])
		}
		// Nothing should be redacted if there was no prefix in the input.
		if !strings.Contains(plaintext, testPrefix.Prefix) && got != plaintext {
			t.Errorf("replacer output %q != input %q, but input has no prefix", got, plaintext)
		}
	})
}

// tokenLike matches testPrefix followed by at least MinBody body bytes.
var tokenLike = regexp.MustCompile(`tok_[a-z0-9.\-_]{8,}`)

func FuzzReplacer(f *testing.F) {
	f.Add(lipsum, 10, "", "", "", "")
	f.Add(lipsum, 10, "ipsum", "", "", "")
	f.Add(lipsum, 10, "ipsum", "sit", "", "")
	f.Add(lipsum, 10, "ipsum dolor", "dolor", "", "")
	f.Add(lipsum, 10, "ipsum", "ipsum dolor", "", "")
	f.Add(lipsum, 10, "ipsum dolor", "dolor sit", "", "")
	f.Add(lipsum, 10, "ipsum", "dolor", "sit", "amet")
	f.Add(lipsum, 10, "a", "e", "i", "o")
	f.Fuzz(func(t *testing.T, plaintext string, split int, a, b, c, d string) {
		// Don't allow empty secrets, whitespace only secrets, or secrets
		// containing a character from the redaction substitution.
		//  - Replacing a secret with '[REDACTED]' may create text that happens
		//    to be another secret.
		//  - Unless disallowed, the fuzzer tends to rapidly find secrets like
		//    "A" (one of the characters in REDACTED).
		secrets := make([]string, 0, 4)
		for _, s := range []string{a, b, c, d} {
			s = strings.TrimSpace(s)
			if s == "" || strings.ContainsAny(s, "[REDACTED]") {
				continue
			}
			secrets = append(secrets, s)
		}

		var sb strings.Builder
		replacer := replacer.New(&sb, secrets, redact.Redacted)

		if split < 0 || split >= len(plaintext) {
			if _, err := fmt.Fprint(replacer, plaintext); err != nil {
				t.Errorf("fmt.Fprint(replacer, %q) error = %v", plaintext, err)
			}
		} else {
			if _, err := fmt.Fprint(replacer, plaintext[:split]); err != nil {
				t.Errorf("fmt.Fprint(replacer, %q) error = %v", plaintext[:split], err)
			}
			if _, err := fmt.Fprint(replacer, plaintext[split:]); err != nil {
				t.Errorf("fmt.Fprint(replacer, %q) error = %v", plaintext[split:], err)
			}
		}
		if err := replacer.Flush(); err != nil {
			t.Errorf("replacer.Flush() = %v", err)
		}
		got := sb.String()

		for _, s := range secrets {
			if strings.Contains(got, s) {
				t.Errorf("replacer output %q contains secret %q", got, s)
			}
		}
	})
}
