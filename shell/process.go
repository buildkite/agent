package shell

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
)

type Process struct {
	// The command to run in the process
	Command *Command

	// Additonal config for the process
	Config *Config

	// The exit status of the process
	exitStatus int
}

func (p *Process) Run() error {
	// Windows has a hard time finding files that are located in folders
	// that you've added dynmically to PATH, so we'll use `AbsolutePath`
	// method (that looks for files in PATH) and use the path from that
	// instead.
	absolutePathToCommand, err := p.Command.AbsolutePath()
	if err != nil {
		return err
	}

	cmd := exec.Command(absolutePathToCommand, p.Command.Args...)

	if p.Command.Env != nil {
		cmd.Env = p.Command.Env.ToSlice()
	}

	if p.Command.Dir != "" {
		cmd.Dir = p.Command.Dir
	}

	if p.Config.PTY {
		// Start our process in a PTY
		pty, err := ptyStart(cmd)
		if err != nil {
			return fmt.Errorf("Failed to start PTY: ", err)
		}

		// Copy the pty to our buffer. This will block until it EOF's
		// or something breaks.
		_, err = io.Copy(p.Config.Writer, pty)
		if e, ok := err.(*os.PathError); ok && e.Err == syscall.EIO {
			// We can safely ignore this error, because it's just
			// the PTY telling us that it closed successfully.
			// See:
			// https://github.com/buildkite/agent/pull/34#issuecomment-46080419
			err = nil
		}
	} else {
		cmd.Stdout = p.Config.Writer
		cmd.Stderr = p.Config.Writer
		cmd.Stdin = nil

		err := cmd.Start()
		if err != nil {
			return err
		}
	}

	// Wait for the command to finish
	waitResult := cmd.Wait()

	// Get the exit status
	// https://github.com/hnakamur/commango/blob/fe42b1cf82bf536ce7e24dceaef6656002e03743/os/executil/executil.go#L29
	if waitResult != nil {
		if err, ok := waitResult.(*exec.ExitError); ok {
			if s, ok := err.Sys().(syscall.WaitStatus); ok {
				p.exitStatus = s.ExitStatus()
			} else {
				return errors.New("Unimplemented for system where exec.ExitError.Sys() is not syscall.WaitStatus.")
			}
		}
	} else {
		p.exitStatus = 0
	}

	return nil
}

func (p *Process) ExitStatus() int {
	return p.exitStatus
}
