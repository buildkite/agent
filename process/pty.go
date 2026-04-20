//go:build !windows

package process

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"
)

func StartPTY(c *exec.Cmd, raw bool) (*os.File, error) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		return nil, err
	}
	defer tty.Close() //nolint:errcheck // Best-effort cleanup.

	if err := pty.Setsize(ptmx, &pty.Winsize{
		Rows: 100,
		Cols: 160,
		X:    0, // unused
		Y:    0, // unused
	}); err != nil {
		ptmx.Close() //nolint:errcheck // Best-effort cleanup.
		return nil, err
	}

	if raw {
		if _, err := term.MakeRaw(int(ptmx.Fd())); err != nil {
			ptmx.Close() //nolint:errcheck // Best-effort cleanup.
			return nil, err
		}
	}

	if c.Stdout == nil {
		c.Stdout = tty
	}
	if c.Stderr == nil {
		c.Stderr = tty
	}
	if c.Stdin == nil {
		c.Stdin = tty
	}

	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.Setsid = true
	c.SysProcAttr.Setctty = true

	if err := c.Start(); err != nil {
		ptmx.Close() //nolint:errcheck // Best-effort cleanup.
		return nil, err
	}

	return ptmx, nil
}
