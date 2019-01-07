// +build windows

package logger

import (
	"os"

	"golang.org/x/sys/windows"
)

// Recent windows versions support ANSI color output if we enable them

func init() {
	stdout := windows.Handle(os.Stdout.Fd())

	err := windows.SetConsoleMode(stdout, windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.ENABLE_PROCESSED_OUTPUT|windows.ENABLE_WRAP_AT_EOL_OUTPUT)
	if err == nil {
		windowsColors = true
	}
}
