package redaction

import (
	"bytes"
	"fmt"
	"testing"
)

func TestRedactorEmpty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	redactor := NewRedactor(&buf, "[REDACTED]", []string{})

	fmt.Fprint(redactor, "Lorem ipsum dolor sit amet")
	redactor.Flush()

	if got, want := buf.String(), "Lorem ipsum dolor sit amet"; got != want {
		t.Errorf("post-redaction buf.String() = %q, want %q", got, want)
	}
}

func TestRedactorSingle(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	redactor := NewRedactor(&buf, "[REDACTED]", []string{"ipsum"})

	fmt.Fprint(redactor, "Lorem ipsum dolor sit amet")
	redactor.Flush()

	if got, want := buf.String(), "Lorem [REDACTED] dolor sit amet"; got != want {
		t.Errorf("post-redaction buf.String() = %q, want %q", got, want)
	}
}

func TestRedactorMulti(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	redactor := NewRedactor(&buf, "[REDACTED]", []string{"ipsum", "amet"})

	fmt.Fprint(redactor, "Lorem ipsum dolor sit amet")
	redactor.Flush()

	if got, want := buf.String(), "Lorem [REDACTED] dolor sit [REDACTED]"; got != want {
		t.Errorf("post-redaction buf.String() = %q, want %q", got, want)
	}
}

func TestRedactorWriteBoundaries(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	redactor := NewRedactor(&buf, "[REDACTED]", []string{"ipsum"})

	redactor.Write([]byte("Lorem ip"))
	redactor.Write([]byte("sum dolor sit amet"))
	redactor.Flush()

	if got, want := buf.String(), "Lorem [REDACTED] dolor sit amet"; got != want {
		t.Errorf("post-redaction buf.String() = %q, want %q", got, want)
	}
}

func TestRedactorResetMidStream(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	redactor := NewRedactor(&buf, "[REDACTED]", []string{"secret1111"})

	// start writing to the stream (no trailing newline, to be extra tricky)
	redactor.Write([]byte("redact secret1111 but don't redact secret2222 until"))

	// update the redactor with a new secret
	redactor.Flush() // manual flush is necessary before Reset
	redactor.Reset([]string{"secret1111", "secret2222"})

	// finish writing
	redactor.Write([]byte(" after secret2222 is added\n"))
	redactor.Flush()

	if got, want := buf.String(), "redact [REDACTED] but don't redact secret2222 until after [REDACTED] is added\n"; got != want {
		t.Errorf("post-redaction buf.String() = %q, want %q", got, want)
	}
}

func TestRedactorSlowLoris(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	redactor := NewRedactor(&buf, "[REDACTED]", []string{"secret1111"})

	redactor.Write([]byte("s"))
	redactor.Write([]byte("e"))
	redactor.Write([]byte("c"))
	redactor.Write([]byte("r"))
	redactor.Write([]byte("e"))
	redactor.Write([]byte("t"))
	redactor.Write([]byte("1"))
	redactor.Write([]byte("1"))
	redactor.Write([]byte("1"))
	redactor.Write([]byte("1"))
	redactor.Flush()

	if got, want := buf.String(), "[REDACTED]"; got != want {
		t.Errorf("post-redaction buf.String() = %q, want %q", got, want)
	}
}

func TestRedactorSubsetSecrets(t *testing.T) {
	t.Parallel()

	/*
		This probably isn't a desired behaviour but I wanted to document it.

		If one of the needles/secrets is a prefix subset of another, only
		the smaller / prefix secret will be redacted.

		I suspect this will not be an issue in practice due to the strings
		we expect to redact.

		If this is a critical issue, this test is NOT required to pass
		for backwards compatibility and SHOULD be changed to test that
		the longer secret string is redacted.
	*/

	var buf bytes.Buffer
	redactor := NewRedactor(&buf, "[REDACTED]", []string{"secret1111", "secret"})

	redactor.Write([]byte("secret1111"))
	redactor.Flush()

	if got, want := buf.String(), "[REDACTED]1111"; got != want {
		t.Errorf("post-redaction buf.String() = %q, want %q", got, want)
	}
}

func TestRedactorMultibyte(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	redactor := NewRedactor(&buf, "[REDACTED]", []string{"ÿ"})

	redactor.Write([]byte("fooÿbar"))
	redactor.Flush()

	if got, want := buf.String(), "foo[REDACTED]bar"; got != want {
		t.Errorf("post-redaction buf.String() = %q, want %q", got, want)
	}
}
