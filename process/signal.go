//go:build !windows
// +build !windows

package process

import (
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

// setupProcessGroup causes the process to be run in its own process group
// This is useful for sending signals to the process and all its children that
// won't be sent to the bootstrap process.
func (p *Process) setupProcessGroup() {
	// PTY mode already creates a process group, using setsid instead of setpgid
	// Attempting to do so again will cause errors.
	// See https://github.com/creack/pty/issues/35#issuecomment-147947212 for more details.
	if p.conf.PTY {
		return
	}

	p.command.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}
}

func (p *Process) postStart() error {
	// a no-op on non-windows
	return nil
}

func (p *Process) terminateProcessGroup() error {
	// Note: terminateProcessGroup is called from within p.Terminate, which
	// holds p.mu.
	p.logger.Debug("[Process] Sending signal SIGKILL to PGID: %d", p.pid)
	return syscall.Kill(-p.pid, syscall.SIGKILL)
}

func (p *Process) interruptProcessGroup() error {
	// Note: interruptProcessGroup is called from within p.Interrupt, which
	// holds p.mu.
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

// SignalString returns the name of the given signal.
// For example, SignalString(syscall.Signal(15)) // "SIGTERM"
func SignalString(s syscall.Signal) string {
	name := unix.SignalName(s)
	if name == "" {
		return strconv.Itoa(int(s))
	}
	return name
}
