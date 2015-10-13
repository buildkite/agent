package agent

import (
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/buildkite/agent/process"
)

// Executes a shell function
func (b Bootstrap) shell(command string, args ...string) (string, string) {
	// Come up with a nice way of showing the command if we need to
	display := strings.Join(append([]string{command}, args...), " ")

	// Execute the command
	c := exec.Command(command, args...)

	// A buffer and multi writer so we can capture shell output
	var buffer bytes.buffer
	multiWriter := io.MultiWriter(&p.buffer, os.Stdout)

	c.Stdout = multiWriter
	c.Stderr = multiWriter

	err := c.Start()
	if err != nil {
		fatalf("There was an error running `%s` (%s)", display, err)
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
