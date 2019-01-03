package process

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/buildkite/agent/logger"
)

// Windows has no concept of parent/child processes or signals. The best we can do
// is create processes inside a "console group" and then send break / ctrl-c events
// to that group. This is superior to walking a process tree to kill each process
// because that relies on each process in that chain still being active.

// See https://docs.microsoft.com/en-us/windows/console/generateconsolectrlevent

var (
	libkernel32                  = syscall.MustLoadDLL("kernel32")
	procSetConsoleCtrlHandler    = libkernel32.MustFindProc("SetConsoleCtrlHandler")
	procGenerateConsoleCtrlEvent = libkernel32.MustFindProc("GenerateConsoleCtrlEvent")
)

const (
	createNewProcessGroupFlag = 0x00000200
)

func StartPTY(c *exec.Cmd) (*os.File, error) {
	return nil, errors.New("PTY is not supported on Windows")
}

func createCommand(cmd string, args ...string) *exec.Cmd {
	execCmd := exec.Command(cmd, args...)
	execCmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_UNICODE_ENVIRONMENT | createNewProcessGroupFlag,
	}
	return execCmd
}

func terminateProcess(p *os.Process, l *logger.Logger) error {
	l.Debug("[Process] Terminating process tree with TASKKILL.EXE PID: %d", p.Pid)

	// taskkill.exe with /F will call TerminateProcess and hard-kill the process and
	// anything left in it's process tree.
	return exec.Command("CMD", "/C", "TASKKILL.EXE", "/F", "/T", "/PID", strconv.Itoa(p.Pid)).Run()
}

func interruptProcess(p *os.Process, l *logger.Logger) error {
	procSetConsoleCtrlHandler.Call(0, 1)
	defer procSetConsoleCtrlHandler.Call(0, 0)
	r1, _, err := procGenerateConsoleCtrlEvent.Call(syscall.CTRL_BREAK_EVENT, uintptr(p.Pid))
	if r1 == 0 {
		return err
	}
	r1, _, err = procGenerateConsoleCtrlEvent.Call(syscall.CTRL_C_EVENT, uintptr(p.Pid))
	if r1 == 0 {
		return err
	}
	return nil
}
