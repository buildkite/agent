package process

import (
	"errors"
	"os"
	"os/exec"
	"strconv"

	"github.com/buildkite/agent/logger"
)

// Windows signal handling isn't a thing unfortunately, so we are limited in what we
// can do with gracefully terminating. Windows also doesn't have process hierarchies
// in the same way that unix does, so killing a process doesn't have any effect on
// the processes it spawned. We use TASKKILL.EXE with the /T flag to traverse processes
// that have a matching ParentProcessID, but even then if a process in the middle
// of the chain has died, we leave a heap of processes hanging around. Future improvements
// might include using terminal groups or job groups, which are a modern windows thing
// that might work for us.

func StartPTY(c *exec.Cmd) (*os.File, error) {
	return nil, errors.New("PTY is not supported on Windows")
}

func createCommand(name string, arg ...string) *exec.Cmd {
	return exec.Command(name, arg...)
}

func terminateProcess(p *os.Process, l *logger.Logger) error {
	l.Debug("[Process] Terminating process tree with TASKKILL.EXE PID: %d", p.Pid)

	// taskkill.exe with /F will call TerminateProcess and hard-kill the process and
	// anything in it's process tree.
	return exec.Command("CMD", "/C", "TASKKILL.EXE", "/F", "/T", "/PID", strconv.Itoa(p.Pid)).Run()
}

func interruptProcess(p *os.Process, l *logger.Logger) error {
	l.Debug("[Process] Interrupting process tree with TASKKILL.EXE PID: %d", p.Pid)

	// taskkill.exe without the /F will use window signalling (WM_STOP, WM_QUIT) to kind
	// of gracefull shutdown windows things that support it.
	return exec.Command("CMD", "/C", "TASKKILL.EXE", "/T", "/PID", strconv.Itoa(p.Pid)).Run()
}
