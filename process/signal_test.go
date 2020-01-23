package process_test

import (
	"syscall"
	"testing"

	"github.com/buildkite/agent/v3/process"
	"github.com/stretchr/testify/assert"
)

func TestSignalString(t *testing.T) {
	for _, row := range []struct {
		n int
		s string
	}{
		{2, "SIGINT"},
		{9, "SIGKILL"},
		{15, "SIGTERM"},
		{100, "100"},
	} {
		assert.Equal(t, row.s, process.SignalString(syscall.Signal(row.n)))
	}
}
