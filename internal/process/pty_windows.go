package process

import (
	"errors"
	"os"
	"os/exec"
)

func StartPTY(c *exec.Cmd, raw bool) (*os.File, error) {
	return nil, errors.New("PTY is not supported on Windows")
}
