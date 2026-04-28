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
		t.Errorf("err error = %v, want nil", err)
	}
	if got, want := n, 5; got != want {
		t.Errorf("n = %d, want %d", got, want)
	}
	if got, want := uint64(0), remaining; got != want {
		t.Errorf("uint64(0) = %d, want %d", got, want)
	}
}

func TestLimitWriter_ExceedsBudget(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	remaining := uint64(3)
	lw := &limitWriter{w: &buf, remainingBytes: &remaining}

	_, err := lw.Write([]byte("hello"))
	if err == nil {
		t.Errorf("err error = %v, want non-nil error", err)
	}
}
