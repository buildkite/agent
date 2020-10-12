// +build windows

package process

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
)

// Windows has no concept of parent/child processes or signals. The best we can do
// is create processes inside a "console group" and then send break / ctrl-c events
// to that group. This is superior to walking a process tree to kill each process
// because that relies on each process in that chain still being active.

// See https://docs.microsoft.com/en-us/windows/console/generateconsolectrlevent

func (p *Process) setupProcessGroup() {
	p.command.SysProcAttr = &windows.SysProcAttr{
		CreationFlags: windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_NEW_PROCESS_GROUP,
	}
}

func (p *Process) terminateProcessGroup() error {
	p.logger.Debug("[Process] Terminating process tree with TASKKILL.EXE PID: %d", p.pid)

	// taskkill.exe with /F will call TerminateProcess and hard-kill the process and
	// anything left in its process tree.
	return exec.Command("CMD", "/C", "TASKKILL.EXE", "/F", "/T", "/PID", strconv.Itoa(p.pid)).Run()
}

func (p *Process) interruptProcessGroup() error {
	// Sends a CTRL-BREAK signal to the process group id, which is the same as the process PID
	// For some reason I cannot fathom, this returns "Incorrect function" in docker for windows
	err := windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(p.pid))
	if err != nil {
		return err
	}
	return nil
}

func GetPgid(pid int) (int, error) {
	return 0, errors.New("Not implemented on Windows")
}

// SignalString returns the name of the given signal.
// e.g. SignalString(syscall.Signal(15)) // "terminated"
func SignalString(s syscall.Signal) string {
	return fmt.Sprintf("%v", s)
}
