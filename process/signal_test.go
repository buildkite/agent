package process_test

import (
	"runtime"
	"syscall"
	"testing"

	"github.com/buildkite/agent/v3/process"
	"github.com/stretchr/testify/assert"
)

func TestSignalStringUnix(t *testing.T) {
	if runtime.GOOS == `windows` {
		t.Skip("Unix signal names are not used on Windows")
	}

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

func TestSignalStringWindows(t *testing.T) {
	if runtime.GOOS != `windows` {
		t.Skip("Windows signal names are not used on Unix")
	}

	for _, row := range []struct {
		n int
		s string
	}{
		{2, "interrupt"},
		{9, "killed"},
		{15, "terminated"},
		{100, "signal 100"},
	} {
		assert.Equal(t, row.s, process.SignalString(syscall.Signal(row.n)))
	}
}
