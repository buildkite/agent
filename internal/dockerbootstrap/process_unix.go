//go:build !windows

package dockerbootstrap

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func isolateProcess(cmd *exec.Cmd) {
	// JobRunner signals the supervisor's entire process group. Keep Docker
	// clients outside it so only the supervisor forwards cancellation, once.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
