// +build !windows

package process

import (
	"os"
	"os/exec"

	"github.com/kr/pty"
)

func StartPTY(c *exec.Cmd) (*os.File, error) {
	return pty.Start(c)
}
