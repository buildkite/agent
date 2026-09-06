//go:build !windows

package dockerbootstrap

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func isolateProcess(cmd *exec.Cmd) {
	// A separate group prevents duplicate cancellation from JobRunner's group
	// signal and the supervisor. Supervisor SIGKILL can orphan both the client
	// and container; recovery requires reconciliation.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
