// +build !windows

package shell

import (
	"os"
	"os/exec"

	"github.com/kr/pty"
)

func ptyStart(c *exec.Cmd) (*os.File, error) {
	return pty.Start(c)
}
