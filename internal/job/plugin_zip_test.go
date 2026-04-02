package job

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLimitWriter_ExactBudget(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	remaining := uint64(5)
	lw := &limitWriter{w: &buf, remainingBytes: &remaining}

	n, err := lw.Write([]byte("hello"))
	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, uint64(0), remaining)
}

func TestLimitWriter_ExceedsBudget(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	remaining := uint64(3)
	lw := &limitWriter{w: &buf, remainingBytes: &remaining}

	_, err := lw.Write([]byte("hello"))
	assert.Error(t, err)
}
