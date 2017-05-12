package shell

import (
	"errors"
	"os"
	"os/exec"
)

func signalProcess(cmd *exec.Cmd, sig os.Signal) error {
	if cmd.Process != nil {
		return errors.New("Process doesn't exist yet")
	}
	return cmd.Process.Signal(sig)
}
