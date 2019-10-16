package bootstrap

import (
	"testing"
	"bytes"
	"fmt"
)

func TestLogFilterSingle(t *testing.T) {
	var buf bytes.Buffer

	filter := NewLogFilter(&buf, "[REDACTED]", []string{"ipsum"})

	fmt.Fprint(filter, "Lorem ipsum dolor sit amet")
	filter.Sync()

	if buf.String() != "Lorem [REDACTED] dolor sit amet" {
		t.Errorf("Redaction failed: %s", buf.String())
	}
}

func TestLogFilterMulti(t *testing.T) {
	var buf bytes.Buffer

	filter := NewLogFilter(&buf, "[REDACTED]", []string{"ipsum", "amet"})

	fmt.Fprint(filter, "Lorem ipsum dolor sit amet")
	filter.Sync()

	if buf.String() != "Lorem [REDACTED] dolor sit [REDACTED]" {
		t.Errorf("Redaction failed: %s", buf.String())
	}
}

func TestLogFilterWriteBoundaries(t *testing.T) {
	var buf bytes.Buffer

	filter := NewLogFilter(&buf, "[REDACTED]", []string{"ipsum"})

	filter.Write([]byte("Lorem ip"))
	filter.Write([]byte("sum dolor sit amet"))
	filter.Sync()

	if buf.String() != "Lorem [REDACTED] dolor sit amet" {
		t.Errorf("Redaction failed: %s", buf.String())
	}
}
