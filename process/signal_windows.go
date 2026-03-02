//go:build windows
// +build windows

package process

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows has no concept of parent/child processes or signals. The best we can do
// is create processes inside a "console group" and then send break / ctrl-c events
// to that group. This is superior to walking a process tree to kill each process
// because that relies on each process in that chain still being active.

// See https://docs.microsoft.com/en-us/windows/console/generateconsolectrlevent

func (p *Process) setupProcessGroup() {
	if p.conf.PTY {
		return
	}
	p.command.SysProcAttr = &windows.SysProcAttr{
		CreationFlags: windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_NEW_PROCESS_GROUP,
	}
	jobHandle, err := newJobObject()
	if err != nil {
		p.logger.Error("Creating Job Object failed: %v", err)
	}
	p.winJobHandle = jobHandle
}

func newJobObject() (uintptr, error) {
	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		handle,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info))); err != nil {
		return 0, err
	}

	return uintptr(handle), nil
}

func (p *Process) postStart() error {
	// convert the pid into a windows process handle. We need particular permissions on the handle
	// for AssignProcessToJobObject to accept it
	pid := uint32(p.command.Process.Pid)
	processPerms := uint32(windows.PROCESS_QUERY_LIMITED_INFORMATION | windows.PROCESS_SET_QUOTA | windows.PROCESS_TERMINATE)
	processHandle, err := windows.OpenProcess(processPerms, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(processHandle)

	err = windows.AssignProcessToJobObject(windows.Handle(p.winJobHandle), processHandle)
	if err != nil {
		return err
	}
	return nil
}

func (p *Process) terminateProcessGroup() error {
	p.logger.Debug("[Process] Terminating process tree by destroying job")
	return windows.CloseHandle(windows.Handle(p.winJobHandle))
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
	return 0, errors.New("not implemented on Windows")
}

// SignalString returns the name of the given signal.
// For example, SignalString(syscall.Signal(15)) // "terminated"
func SignalString(s syscall.Signal) string {
	return fmt.Sprintf("%v", s)
}
