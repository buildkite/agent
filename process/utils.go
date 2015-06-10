// +build !windows

package process

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/kr/pty"
)

func StartPTY(c *exec.Cmd) (*os.File, error) {
	return pty.Start(c)
}

func PrepareCommandProcess(p *Process) {
	// Children of the forked process will inherit its process group
	// This is to make sure that all grandchildren dies when this Process instance is killed
	p.command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
