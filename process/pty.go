//go:build !windows
// +build !windows

package process

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

func StartPTY(c *exec.Cmd) (*os.File, error) {
	return pty.StartWithSize(c, &pty.Winsize{
		Rows: 100,
		Cols: 160,
		X:    0, // unused
		Y:    0, // unused
	})
}
