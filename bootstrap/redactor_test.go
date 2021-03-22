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
