//go:build !windows
// +build !windows

package shell

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func signalProcess(cmd *exec.Cmd, sig os.Signal) error {
	if cmd.Process == nil {
		return errors.New("Process doesn't exist yet")
	}

	// If possible, send to the process group (linux/darwin/bsd)
	if ssig, ok := sig.(syscall.Signal); ok {
		return syscall.Kill(-cmd.Process.Pid, ssig)
	}

	return cmd.Process.Signal(sig)
}
