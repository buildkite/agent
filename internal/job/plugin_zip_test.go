package job

import (
	"bytes"
	"testing"
)

func TestLimitWriter_ExactBudget(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	remaining := uint64(5)
	lw := &limitWriter{w: &buf, remainingBytes: &remaining}

	n, err := lw.Write([]byte("hello"))
	if err != nil {
		t.Errorf("lw.Write([]byte(\"hello\")) error = %v, want nil", err)
	}
	if got, want := n, 5; got != want {
		t.Errorf("lw.Write([]byte(\"hello\")) = %d, want %d", got, want)
	}
	if got, want := remaining, uint64(0); got != want {
		t.Errorf("uint64(%d) = %d, want %d", 5, got, want)
	}
}

func TestLimitWriter_ExceedsBudget(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	remaining := uint64(3)
	lw := &limitWriter{w: &buf, remainingBytes: &remaining}

	_, err := lw.Write([]byte("hello"))
	if err == nil {
		t.Errorf("lw.Write([]byte(\"hello\")) error = %v, want non-nil error", err)
	}
}
