// +build linux,netbsd,freebsd,dragonfly darwin,386 darwin,amd64 openbsd,386 openbsd,amd64

package process

import (
	"os"
	"os/exec"

	"github.com/kr/pty"
)

func StartPTY(c *exec.Cmd) (*os.File, error) {
	return pty.Start(c)
}
