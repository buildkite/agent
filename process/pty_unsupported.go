// These are all the os/arch pairs that github.com/kr/pty fails compilation under.
// pty.go has the complement of this list.

// +build !linux,!netbsd,!freebsd,!dragonfly
// +build !darwin !386
// +build !darwin !amd64
// +build !openbsd !386
// +build !openbsd !amd64

package process

import (
	"errors"
	"os"
	"os/exec"
)

func StartPTY(c *exec.Cmd) (*os.File, error) {
	return nil, errors.New("PTY is not supported on this platform")
}
