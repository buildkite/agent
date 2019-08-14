// +build !windows

package process

import (
	"syscall"
)

func (p *Process) setupProcessGroup() {
	// See https://github.com/kr/pty/issues/35 for context
	if !p.conf.PTY {
		p.command.SysProcAttr = &syscall.SysProcAttr{
			Setpgid: true,
			Pgid:    0,
		}
	}
}

func (p *Process) terminateProcessGroup() error {
	p.logger.Debug("[Process] Sending signal SIGKILL to PGID: %d", p.pid)
	return syscall.Kill(-p.pid, syscall.SIGKILL)
}

func (p *Process) interruptProcessGroup() error {
	intSignal := p.conf.InterruptSignal

	// TODO: this should be SIGINT, but will be a breaking change
	if intSignal == Signal(0) {
		intSignal = SIGTERM
	}

	p.logger.Debug("[Process] Sending signal %s to PGID: %d", intSignal, p.pid)
	return syscall.Kill(-p.pid, syscall.Signal(intSignal))
}

func GetPgid(pid int) (int, error) {
	return syscall.Getpgid(pid)
}
