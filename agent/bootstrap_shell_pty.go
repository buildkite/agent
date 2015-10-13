package agent

import (
	"os"
	"os/exec"

	"github.com/kr/pty"
)

func (b Bootstrap) shellPTYStart(c *exec.Cmd) (*os.File, error) {
	return pty.Start(c)
}
