//go:build unix

package clicommand

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// dupFd returns a duplicate of f's file descriptor, which the caller owns.
func dupFd(t *testing.T, f *os.File) uintptr {
	t.Helper()

	fd, err := unix.Dup(int(f.Fd()))
	if err != nil {
		t.Fatalf("unix.Dup(%d) = %v", f.Fd(), err)
	}
	return uintptr(fd)
}
