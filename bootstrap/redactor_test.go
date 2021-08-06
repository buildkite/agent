package bootstrap

import (
	"bytes"
	"fmt"
	"testing"
)

func TestRedactorSingle(t *testing.T) {
	var buf bytes.Buffer

	redactor := NewRedactor(&buf, "[REDACTED]", []string{"ipsum"})

	fmt.Fprint(redactor, "Lorem ipsum dolor sit amet")
	redactor.Flush()

	if buf.String() != "Lorem [REDACTED] dolor sit amet" {
		t.Errorf("Redaction failed: %s", buf.String())
	}
}

func TestRedactorMulti(t *testing.T) {
	var buf bytes.Buffer

	redactor := NewRedactor(&buf, "[REDACTED]", []string{"ipsum", "amet"})

	fmt.Fprint(redactor, "Lorem ipsum dolor sit amet")
	redactor.Flush()

	if buf.String() != "Lorem [REDACTED] dolor sit [REDACTED]" {
		t.Errorf("Redaction failed: %s", buf.String())
	}
}

func TestRedactorWriteBoundaries(t *testing.T) {
	var buf bytes.Buffer

	redactor := NewRedactor(&buf, "[REDACTED]", []string{"ipsum"})

	redactor.Write([]byte("Lorem ip"))
	redactor.Write([]byte("sum dolor sit amet"))
	redactor.Flush()

	if buf.String() != "Lorem [REDACTED] dolor sit amet" {
		t.Errorf("Redaction failed: %s", buf.String())
	}
}

func TestRedactorResetMidStream(t *testing.T) {
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

	if buf.String() != "redact [REDACTED] but don't redact secret2222 until after [REDACTED] is added\n" {
		t.Errorf("Redaction failed: %s", buf.String())
	}
}

func TestRedactorSlowLoris(t *testing.T) {
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

	if buf.String() != "[REDACTED]" {
		t.Errorf("Redaction failed: %s", buf.String())
	}
}

func TestRedactorSubsetSecrets(t *testing.T) {
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

	if buf.String() != "[REDACTED]1111" {
		t.Errorf("Redaction failed: %s", buf.String())
	}
}

func TestRedactorLatin1(t *testing.T) {
	var buf bytes.Buffer
	redactor := NewRedactor(&buf, "[REDACTED]", []string{"ÿ"})

	redactor.Write([]byte("foo"))
	redactor.Flush()
}
