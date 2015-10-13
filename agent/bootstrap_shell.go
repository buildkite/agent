package agent

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/buildkite/agent/process"
	"github.com/kr/pty"
)

// Executes a shell function
func (b Bootstrap) shell(env map[string]string, command string, args ...string) (string, string) {
	// Come up with a nice way of showing the command if we need to
	display := strings.Join(append([]string{command}, args...), " ")

	// Execute the command
	c := exec.Command(command, args...)
	c.Env = append(os.Environ(), convertEnvMapIntoSlice(env)...)

	// A buffer and multi writer so we can capture shell output
	var buffer bytes.Buffer
	multiWriter := io.MultiWriter(&buffer, os.Stdout)

	if b.RunInPty {
		// Start our process
		f, err := pty.Start(c)
		if err != nil {
			fatalf("There was an error running `%s` on a PTY (%s)", display, err)
		}

		// Copy the pty to our buffer. This will block until it
		// EOF's or something breaks.
		_, err = io.Copy(multiWriter, f)
		if e, ok := err.(*os.PathError); ok && e.Err == syscall.EIO {
			// We can safely ignore this error, because
			// it's just the PTY telling us that it closed
			// successfully.  See:
			// https://github.com/buildkite/agent/pull/34#issuecomment-46080419
			err = nil
		}
	} else {
		c.Stdout = multiWriter
		c.Stderr = multiWriter

		err := c.Start()
		if err != nil {
			fatalf("There was an error running `%s` (%s)", display, err)
		}
	}

	// Wait for the command to finish
	waitResult := c.Wait()

	// Get the exit status
	exitStatus, err := process.GetExitStatusFromWaitResult(waitResult)
	if err != nil {
		fatalf("There was an error getting the exit status for `%s` (%s)", display, err)
	}

	return exitStatus, buffer.String()
}
