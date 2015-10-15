package shell

import (
	"fmt"
	"os"
	"os/exec"
)

func ptyStart(c *exec.Cmd) (*os.File, error) {
	return nil, fmt.Errorf("Unsupported")
}
