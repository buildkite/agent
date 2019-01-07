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
	procGenerateConsoleCtrlEvent = libkernel32.MustFindProc("GenerateConsoleCtrlEvent")
)

const (
	createNewProcessGroupFlag = 0x00000200
)

func SetupProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_UNICODE_ENVIRONMENT | createNewProcessGroupFlag,
	}
}

func TerminateProcessGroup(p *os.Process, l *logger.Logger) error {
	l.Debug("[Process] Terminating process tree with TASKKILL.EXE PID: %d", p.Pid)

	// taskkill.exe with /F will call TerminateProcess and hard-kill the process and
	// anything left in it's process tree.
	return exec.Command("CMD", "/C", "TASKKILL.EXE", "/F", "/T", "/PID", strconv.Itoa(p.Pid)).Run()
}

func InterruptProcessGroup(p *os.Process, l *logger.Logger) error {
	// Sends a CTRL-BREAK signal to the process group id, which is the same as the process PID
	// For some reason I cannot fathom, this returns "Incorrect function" in docker for windows
	r1, _, err := procGenerateConsoleCtrlEvent.Call(syscall.CTRL_BREAK_EVENT, uintptr(p.Pid))
	if r1 == 0 {
		return err
	}
	return nil
}

func GetPgid(pid int) (int, error) {
	return 0, errors.New("Not implemented on Windows")
}
