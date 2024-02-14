package replacer_test

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/redact"
	"github.com/buildkite/agent/v3/internal/replacer"
	"github.com/google/go-cmp/cmp"
)

const lipsum = "Lorem ipsum dolor sit amet"

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
		test := test
		// Write input in a single Write call
		t.Run("One write;"+test.desc, func(t *testing.T) {
			t.Parallel()

			var buf strings.Builder
			replacer := replacer.New(&buf, test.needles, redact.Redact)
			fmt.Fprint(replacer, lipsum)
			replacer.Flush()

			if got, want := buf.String(), test.want; got != want {
				t.Errorf("post-redaction(needles = %q) buf.String() = %q, want %q", test.needles, got, want)
			}
		})

		// "Slow Loris": write one byte at a time
		t.Run("Many writes;"+test.desc, func(t *testing.T) {
			t.Parallel()

			var buf strings.Builder
			replacer := replacer.New(&buf, test.needles, redact.Redact)
			for _, c := range []byte(lipsum) {
				replacer.Write([]byte{c})
			}
			replacer.Flush()

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
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()
			var buf strings.Builder

			replacer := replacer.New(&buf, test.needles, redact.Redact)

			for _, input := range test.inputs {
				fmt.Fprint(replacer, input)
			}
			replacer.Flush()

			if got, want := buf.String(), test.want; got != want {
				t.Errorf("post-redaction(needles = %q) buf.String() = %q, want %q", test.needles, got, want)
			}
		})
	}
}

func TestReplacerResetMidStream(t *testing.T) {
	t.Parallel()

	var buf strings.Builder
	replacer := replacer.New(&buf, []string{"secret1111"}, redact.Redact)

	// start writing to the stream (no trailing newline, to be extra tricky)
	replacer.Write([]byte("redact secret1111 but don't redact secret2222 until"))

	// update the replacer with a new secret
	//replacer.Flush() // manual flush is NOT necessary before Reset
	replacer.Reset([]string{"secret1111", "secret2222"})

	// finish writing
	replacer.Write([]byte(" after secret2222 is added\n"))
	replacer.Flush()

	if got, want := buf.String(), "redact [REDACTED] but don't redact secret2222 until after [REDACTED] is added\n"; got != want {
		t.Errorf("post-redaction buf.String() = %q, want %q", got, want)
	}
}

func TestReplacerMultibyte(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	replacer := replacer.New(&buf, []string{"ÿ"}, redact.Redact)

	replacer.Write([]byte("fooÿbar"))
	replacer.Flush()

	if got, want := buf.String(), "foo[REDACTED]bar"; got != want {
		t.Errorf("post-redaction buf.String() = %q, want %q", got, want)
	}
}

func TestReplacerMultiLine(t *testing.T) {
	t.Parallel()

	var buf strings.Builder

	replacer := replacer.New(&buf, []string{"-----BEGIN OPENSSH PRIVATE KEY-----\nasdf\n-----END OPENSSH PRIVATE KEY-----\n"}, redact.Redact)

	fmt.Fprintln(replacer, "lalalala")
	fmt.Fprintln(replacer, "-----BEGIN OPENSSH PRIVATE KEY-----")
	fmt.Fprintln(replacer, "asdf")
	fmt.Fprintln(replacer, "-----END OPENSSH PRIVATE KEY-----")
	fmt.Fprintln(replacer, "lalalala")
	replacer.Flush()

	want := "lalalala\n[REDACTED]lalalala\n"

	if diff := cmp.Diff(buf.String(), want); diff != "" {
		t.Errorf("post-redaction diff (-got +want):\n%s", diff)
	}
}

func BenchmarkReplacer(b *testing.B) {
	b.ResetTimer()
	r := replacer.New(io.Discard, bigLipsumSecrets, redact.Redact)
	for range b.N {
		fmt.Fprintln(r, bigLipsum)
	}
	r.Flush()
}

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
		// Don't allow empty secrets, or secrets containing a character from
		// the redaction substitution.
		//  - Replacing a secret with '[REDACTED]' may create text that happens
		//    to be another secret.
		//  - Unless disallowed, the fuzzer tends to rapidly find secrets like
		//    "A" (one of the characters in REDACTED).
		secrets := make([]string, 0, 4)
		for _, s := range []string{a, b, c, d} {
			if s == "" || strings.ContainsAny(s, "[REDACTED]") {
				continue
			}
			secrets = append(secrets, s)
		}

		var sb strings.Builder
		replacer := replacer.New(&sb, secrets, redact.Redact)
		if split < 0 || split >= len(plaintext) {
			fmt.Fprint(replacer, plaintext)
		} else {
			fmt.Fprint(replacer, plaintext[:split])
			fmt.Fprint(replacer, plaintext[split:])
		}
		replacer.Flush()
		got := sb.String()

		for _, s := range secrets {
			if strings.Contains(got, s) {
				t.Errorf("replacer output %q contains secret %q", got, s)
			}
		}
	})
}
