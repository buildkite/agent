package process

import (
	"os"
	"os/exec"
)

func Signal(cmd *exec.Cmd, sig os.Signal) error {
	return cmd.Process.Signal(sig)
}
