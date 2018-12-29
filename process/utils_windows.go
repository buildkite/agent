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
	logger.Debug("[Process] Terminating process tree with TASKKILL.EXE PID: %d", p.Pid)
	return exec.Command("CMD", "/C", "TASKKILL.EXE", "/F", "/T", "/PID", strconv.Itoa(p.Pid)).Run()
}

func interruptProcess(p *os.Process) error {
	logger.Debug("[Process] Interrupting process tree with TASKKILL.EXE PID: %d", p.Pid)
	return exec.Command("CMD", "/C", "TASKKILL.EXE", "/T", "/PID", strconv.Itoa(p.Pid)).Run()
}
