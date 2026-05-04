//go:build windows

package logger

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// Windows 10 Build 16257 added support for ANSI color output if we enable them

func init() {
	// Enable ANSI on both stdout and stderr. Diagnostic logs go to
	// stderr (post-slog migration); some legacy paths still write
	// colored output to stdout.
	stdoutOK := enableANSI(os.Stdout.Fd())
	stderrOK := enableANSI(os.Stderr.Fd())
	windowsColors = stdoutOK && stderrOK
}

func enableANSI(fd uintptr) bool {
	var mode uint32
	h := windows.Handle(fd)
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return false
	}
	// See https://docs.microsoft.com/en-us/windows/console/getconsolemode
	mode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	if err := windows.SetConsoleMode(h, mode); err != nil {
		fmt.Printf("Error setting console mode: %v\n", err)
		return false
	}
	return true
}
