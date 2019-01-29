// +build !windows

package process

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/buildkite/agent/logger"
)

func SetupProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}
}

func TerminateProcessGroup(p *os.Process, l *logger.Logger) error {
	l.Debug("[Process] Sending signal SIGKILL to PGID: %d", p.Pid)
	return syscall.Kill(-p.Pid, syscall.SIGKILL)
}

func InterruptProcessGroup(p *os.Process, l *logger.Logger) error {
	l.Debug("[Process] Sending signal SIGTERM to PGID: %d", p.Pid)

	// TODO: this should be SIGINT, but will be a breaking change
	return syscall.Kill(-p.Pid, syscall.SIGTERM)
}

func GetPgid(pid int) (int, error) {
	return syscall.Getpgid(pid)
}
