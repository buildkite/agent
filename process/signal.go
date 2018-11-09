// +build !windows

package process

import (
	"os"
	"os/exec"
	"syscall"
)

func Signal(cmd *exec.Cmd, sig os.Signal) error {
	if ssig, ok := sig.(syscall.Signal); ok {
		// If possible, send to the process group (linux/darwin/bsd)
		return syscall.Kill(-cmd.Process.Pid, ssig)
	}

	return cmd.Process.Signal(sig)
}
