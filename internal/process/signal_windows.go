//go:build windows

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

// What's the deal with CTRL_C_EVENT ?
//
// Unlike CTRL_BREAK_EVENT, it can only be sent to the *current console window*.
// So, naïvely, we can only use it to self-signal. There's a workaround...
//
// Firstly we would need to create the process with CREATE_NEW_CONSOLE, spawning
// another black console rectangle on the screen for each process. We _could_
// hide it immediately (GetConsoleWindow(pid), ShowWindow(hWnd, SW_HIDE)) but
// the window would flash briefly. Probably no big issue. (Also GetConsoleWindow
// is deprecated.)
//
// But then, when we want to interrupt, we have this Rube-Cronenbergian set of
// Win32 calls:
//
// 1. detach from the current console (FreeConsole)
// 2. attach to the console of this process (AttachConsole)
// 3. add the control handler (SetConsoleCtrlHandler(NULL, TRUE))
// 4. send the CTRL_C_EVENT
// 5. detach from this process's console
// 6. attach back to the original console
// 7. remove the control handler (SetConsoleCtrlHandler(NULL, FALSE))
//
// If you do this in the wrong way, with the wrong amount of time between each
// call, it doesn't work or you end up sending Ctrl-C to your own console.
// It's a PITA.

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
	_, err = windows.SetInformationJobObject(
		handle,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		return 0, err
	}

	return uintptr(handle), nil
}

func (p *Process) postStart() error {
	// convert the pid into a windows process handle. We need particular permissions on the handle
	// for AssignProcessToJobObject to accept it
	pid := uint32(p.pid())
	processPerms := uint32(windows.PROCESS_QUERY_LIMITED_INFORMATION | windows.PROCESS_SET_QUOTA | windows.PROCESS_TERMINATE)
	processHandle, err := windows.OpenProcess(processPerms, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(processHandle)

	return windows.AssignProcessToJobObject(windows.Handle(p.winJobHandle), processHandle)
}

func (p *Process) terminateProcessGroup() error {
	p.logger.Debug("[Process] Terminating process tree by destroying job")
	return windows.CloseHandle(windows.Handle(p.winJobHandle))
}

func (p *Process) interruptProcessGroup() error {
	// Send Ctrl-Break to the process group id, which is the same as the process PID
	// This may return "Incorrect function" in docker for windows
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(p.pid()))
}

func GetPgid(pid int) (int, error) {
	return 0, errors.New("not implemented on Windows")
}

// SignalString returns the name of the given signal.
// For example, SignalString(syscall.Signal(15)) // "terminated"
func SignalString(s syscall.Signal) string { return fmt.Sprint(s) }
