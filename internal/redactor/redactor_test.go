package redactor

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

const lipsum = "Lorem ipsum dolor sit amet"

func TestRedactorLoremIpsum(t *testing.T) {
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
			redactor := New(&buf, "[REDACTED]", test.needles)
			fmt.Fprint(redactor, lipsum)
			redactor.Flush()

			if got, want := buf.String(), test.want; got != want {
				t.Errorf("post-redaction(needles = %q) buf.String() = %q, want %q", test.needles, got, want)
			}
		})

		// "Slow Loris": write one byte at a time
		t.Run("Many writes;"+test.desc, func(t *testing.T) {
			t.Parallel()

			var buf strings.Builder
			redactor := New(&buf, "[REDACTED]", test.needles)
			for _, c := range []byte(lipsum) {
				redactor.Write([]byte{c})
			}
			redactor.Flush()

			if got, want := buf.String(), test.want; got != want {
				t.Errorf("post-redaction(needles = %q) buf.String() = %q, want %q", test.needles, got, want)
			}
		})
	}
}

func TestRedactorWriteBoundaries(t *testing.T) {
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
			var buf strings.Builder

			redactor := New(&buf, "[REDACTED]", test.needles)

			for _, input := range test.inputs {
				fmt.Fprint(redactor, input)
			}
			redactor.Flush()

			if got, want := buf.String(), test.want; got != want {
				t.Errorf("post-redaction(needles = %q) buf.String() = %q, want %q", test.needles, got, want)
			}
		})
	}
}

func TestRedactorResetMidStream(t *testing.T) {
	t.Parallel()

	var buf strings.Builder
	redactor := New(&buf, "[REDACTED]", []string{"secret1111"})

	// start writing to the stream (no trailing newline, to be extra tricky)
	redactor.Write([]byte("redact secret1111 but don't redact secret2222 until"))

	// update the redactor with a new secret
	//redactor.Flush() // manual flush is NOT necessary before Reset
	redactor.Reset([]string{"secret1111", "secret2222"})

	// finish writing
	redactor.Write([]byte(" after secret2222 is added\n"))
	redactor.Flush()

	if got, want := buf.String(), "redact [REDACTED] but don't redact secret2222 until after [REDACTED] is added\n"; got != want {
		t.Errorf("post-redaction buf.String() = %q, want %q", got, want)
	}
}

func TestRedactorMultibyte(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	redactor := New(&buf, "[REDACTED]", []string{"ÿ"})

	redactor.Write([]byte("fooÿbar"))
	redactor.Flush()

	if got, want := buf.String(), "foo[REDACTED]bar"; got != want {
		t.Errorf("post-redaction buf.String() = %q, want %q", got, want)
	}
}

func TestRedactorMultiLine(t *testing.T) {
	t.Parallel()

	var buf strings.Builder

	redactor := New(&buf, "[REDACTED]", []string{"-----BEGIN OPENSSH PRIVATE KEY-----\nasdf\n-----END OPENSSH PRIVATE KEY-----\n"})

	fmt.Fprintln(redactor, "lalalala")
	fmt.Fprintln(redactor, "-----BEGIN OPENSSH PRIVATE KEY-----")
	fmt.Fprintln(redactor, "asdf")
	fmt.Fprintln(redactor, "-----END OPENSSH PRIVATE KEY-----")
	fmt.Fprintln(redactor, "lalalala")
	redactor.Flush()

	want := "lalalala\n[REDACTED]lalalala\n"

	if diff := cmp.Diff(buf.String(), want); diff != "" {
		t.Errorf("post-redaction diff (-got +want):\n%s", diff)
	}
}

func BenchmarkRedactor(b *testing.B) {
	b.ResetTimer()
	r := New(io.Discard, "[REDACTED]", bigLipsumSecrets)
	for n := 0; n < b.N; n++ {
		fmt.Fprintln(r, bigLipsum)
	}
	r.Flush()
}

func FuzzRedactor(f *testing.F) {
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
		redactor := New(&sb, "[REDACTED]", secrets)
		if split < 0 || split >= len(plaintext) {
			fmt.Fprint(redactor, plaintext)
		} else {
			fmt.Fprint(redactor, plaintext[:split])
			fmt.Fprint(redactor, plaintext[split:])
		}
		redactor.Flush()
		got := sb.String()

		for _, s := range secrets {
			if strings.Contains(got, s) {
				t.Errorf("redactor output %q contains secret %q", got, s)
			}
		}
	})
}
