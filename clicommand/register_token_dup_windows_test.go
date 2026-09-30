//go:build windows

package clicommand

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

// dupFd returns a duplicate of f's handle, which the caller owns.
func dupFd(t *testing.T, f *os.File) uintptr {
	t.Helper()

	proc := windows.CurrentProcess()
	var h windows.Handle
	if err := windows.DuplicateHandle(proc, windows.Handle(f.Fd()), proc, &h, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		t.Fatalf("windows.DuplicateHandle(%d) = %v", f.Fd(), err)
	}
	return uintptr(h)
}
