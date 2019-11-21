// These are all the os/arch pairs that github.com/kr/pty compiles successfully under.
// pty_unsupported.go has the complement of this list.

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
