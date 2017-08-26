package shell

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/buildkite/agent/process"
)

// Subprocess is a simpler version of process.Process specifically for the bootstrap
// to use to create sub-processes
type Subprocess struct {
	// The command to run in the process
	Command *exec.Cmd

	// Whether or not the command should be run in a PTY
	PTY bool

	// The exit status of the process
	exitStatus int
}

// Run runs the command in a new process and writes all output to the provided Writer
func (p *Subprocess) Run(w io.Writer) error {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGQUIT)

	go func() {
		// forward signals to the process
		for sig := range signals {
			if err := signalProcess(p.Command, sig); err != nil {
				log.Println("Error passing signal to child process", err)
			}
		}
	}()
	defer signal.Stop(signals)

	if p.PTY {
		pty, err := process.StartPTY(p.Command)
		if err != nil {
			return fmt.Errorf("Failed to start PTY (%v)", err)
		}

		// Copy the pty to our buffer. This will block until it EOF's
		// or something breaks.
		_, err = io.Copy(w, pty)
		if e, ok := err.(*os.PathError); ok && e.Err == syscall.EIO {
			// We can safely ignore this error, because it's just
			// the PTY telling us that it closed successfully.
			// See:
			// https://github.com/buildkite/agent/pull/34#issuecomment-46080419
			err = nil
		}
	} else {
		p.Command.Stdout = w
		p.Command.Stderr = w
		p.Command.Stdin = nil

		err := p.Command.Start()
		if err != nil {
			return err
		}
	}

	// Wait for the command to finish
	waitResult := p.Command.Wait()

	// Get the exit status
	// https://github.com/hnakamur/commango/blob/fe42b1cf82bf536ce7e24dceaef6656002e03743/os/executil/executil.go#L29
	if waitResult != nil {
		if err, ok := waitResult.(*exec.ExitError); ok {
			if s, ok := err.Sys().(syscall.WaitStatus); ok {
				p.exitStatus = s.ExitStatus()
			} else {
				return errors.New("Unimplemented for system where exec.ExitError.Sys() is not syscall.WaitStatus")
			}
		}
	} else {
		p.exitStatus = 0
	}

	return nil
}

// RunAndOutput runs the command in a new process and returns all output as a string
func (p *Subprocess) RunAndOutput() (string, error) {
	var buffer bytes.Buffer

	if err := p.Run(&buffer); err != nil {
		return "", err
	}

	return strings.TrimSpace(buffer.String()), nil
}

// ExitStatus returns the integer exitcode from the last run
func (p *Subprocess) ExitStatus() int {
	return p.exitStatus
}

// String returns a human-friendly string of the command and arguments
func (p *Subprocess) String() string {
	return process.FormatCommand(p.Command.Path, p.Command.Args)
}
