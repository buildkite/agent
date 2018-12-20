package process

import (
	"errors"
	"os"
	"os/exec"
	"strconv"

	"github.com/buildkite/agent/logger"
)

func StartPTY(c *exec.Cmd) (*os.File, error) {
	return nil, errors.New("PTY is not supported on Windows")
}

func createCommand(name string, arg ...string) *exec.Cmd {
	return exec.Command(name, arg...)
}

func terminateProcess(p *os.Process) error {
	logger.Debug("[Process] Killing process with PID: %d", p.Pid)
	return p.Kill()
}

func interruptProcess(p *os.Process) error {
	logger.Debug("[Process] Killing process tree with TASKKILL.EXE PID: %d", p.Pid)
	// Sending Interrupt on Windows is not implemented.
	// https://golang.org/src/os/exec.go?s=3842:3884#L110
	return exec.Command("CMD", "/C", "TASKKILL", "/F", "/T", "/PID", strconv.Itoa(p.Pid)).Run()
}
