// +build !windows

package process

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/buildkite/agent/logger"
	"github.com/kr/pty"
)

func StartPTY(c *exec.Cmd) (*os.File, error) {
	return pty.Start(c)
}

func createCommand(name string, arg ...string) *exec.Cmd {
	return exec.Command(name, arg...)
}

func terminateProcess(p *os.Process, l *logger.Logger) error {
	l.Debug("[Process] Sending signal SIGKILL to PID: %d", p.Pid)
	return p.Signal(syscall.SIGKILL)
}

func interruptProcess(p *os.Process, l *logger.Logger) error {
	l.Debug("[Process] Sending signal SIGTERM to PID: %d", p.Pid)

	// TODO: this should be SIGINT, but will be a breaking change
	return p.Signal(syscall.SIGTERM)
}
