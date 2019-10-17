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
	redactor.Sync()

	if buf.String() != "Lorem [REDACTED] dolor sit amet" {
		t.Errorf("Redaction failed: %s", buf.String())
	}
}

func TestRedactorMulti(t *testing.T) {
	var buf bytes.Buffer

	redactor := NewRedactor(&buf, "[REDACTED]", []string{"ipsum", "amet"})

	fmt.Fprint(redactor, "Lorem ipsum dolor sit amet")
	redactor.Sync()

	if buf.String() != "Lorem [REDACTED] dolor sit [REDACTED]" {
		t.Errorf("Redaction failed: %s", buf.String())
	}
}

func TestRedactorWriteBoundaries(t *testing.T) {
	var buf bytes.Buffer

	redactor := NewRedactor(&buf, "[REDACTED]", []string{"ipsum"})

	redactor.Write([]byte("Lorem ip"))
	redactor.Write([]byte("sum dolor sit amet"))
	redactor.Sync()

	if buf.String() != "Lorem [REDACTED] dolor sit amet" {
		t.Errorf("Redaction failed: %s", buf.String())
	}
}
