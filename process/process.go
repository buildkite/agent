package process

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/buildkite/agent/logger"
)

type Process struct {
	Pid        int
	PTY        bool
	Timestamp  bool
	Script     []string
	Env        []string
	ExitStatus string

	// Handler is called with each line of output
	Handler func(string)

	command       *exec.Cmd
	mu            sync.Mutex
	started, done chan struct{}
}

// Start executes the command and blocks until it finishes
func (p *Process) Start() error {
	if p.command != nil {
		return fmt.Errorf("Process is already running")
	}

	p.command = exec.Command(p.Script[0], p.Script[1:]...)

	// Create channels for signalling started and done
	p.mu.Lock()
	if p.done == nil {
		p.done = make(chan struct{})
	}
	if p.started == nil {
		p.started = make(chan struct{})
	}
	p.mu.Unlock()

	// Copy the current processes ENV and merge in the new ones. We do this
	// so the sub process gets PATH and stuff. We merge our path in over
	// the top of the current one so the ENV from Buildkite and the agent
	// take precedence over the agent
	currentEnv := os.Environ()
	p.command.Env = append(currentEnv, p.Env...)

	var waitGroup sync.WaitGroup

	lineReaderPipe, lineWriterPipe := io.Pipe()

	// Toggle between running in a pty
	if p.PTY {
		pty, err := StartPTY(p.command)
		if err != nil {
			p.ExitStatus = "1"
			return err
		}

		p.Pid = p.command.Process.Pid

		// Signal waiting consumers in Started() by closing the started channel
		close(p.started)

		waitGroup.Add(1)

		go func() {
			logger.Debug("[Process] Starting to copy PTY to the buffer")

			// Copy the pty to our buffer. This will block until it
			// EOF's or something breaks.
			_, err = io.Copy(lineWriterPipe, pty)
			if e, ok := err.(*os.PathError); ok && e.Err == syscall.EIO {
				// We can safely ignore this error, because
				// it's just the PTY telling us that it closed
				// successfully.  See:
				// https://github.com/buildkite/agent/pull/34#issuecomment-46080419
				err = nil
			}

			if err != nil {
				logger.Error("[Process] PTY output copy failed with error: %T: %v", err, err)
			} else {
				logger.Debug("[Process] PTY has finished being copied to the buffer")
			}

			waitGroup.Done()
		}()
	} else {
		p.command.Stdout = lineWriterPipe
		p.command.Stderr = lineWriterPipe
		p.command.Stdin = nil

		err := p.command.Start()
		if err != nil {
			p.ExitStatus = "1"
			return err
		}

		p.Pid = p.command.Process.Pid

		// Signal waiting consumers in Started() by closing the started channel
		close(p.started)
	}

	logger.Info("[Process] Process is running with PID: %d", p.Pid)

	if p.Handler != nil {
		// Add the scanner the waitGroup
		waitGroup.Add(1)

		// Start the Scanner
		go func() {
			defer waitGroup.Done()
			if err := ScanLines(lineReaderPipe, p.Handler); err != nil {
				logger.Error("[Process] Scanner failed with %v", err)
			}
		}()
	} else {
		go io.Copy(ioutil.Discard, lineReaderPipe)
	}

	// Wait until the process has finished. The returned error is nil if the command runs,
	// has no problems copying stdin, stdout, and stderr, and exits with a zero exit status.
	waitResult := p.command.Wait()

	// Close the line writer pipe
	lineWriterPipe.Close()

	// Signal waiting consumers in Done() by closing the done channel
	close(p.done)

	// Find the exit status of the script
	p.ExitStatus = getExitStatus(waitResult)

	logger.Info("Process with PID: %d finished with Exit Status: %s", p.Pid, p.ExitStatus)

	// Sometimes (in docker containers) io.Copy never seems to finish. This is a mega
	// hack around it. If it doesn't finish after 1 second, just continue.
	logger.Debug("[Process] Waiting for routines to finish")
	err := timeoutWait(&waitGroup)
	if err != nil {
		logger.Debug("[Process] Timed out waiting for wait group: (%T: %v)", err, err)
	}

	// No error occurred so we can return nil
	return nil
}

// Done returns a channel that is closed when the process finishes
func (p *Process) Done() <-chan struct{} {
	p.mu.Lock()
	// We create this here in case this is called before Start()
	if p.done == nil {
		p.done = make(chan struct{})
	}
	d := p.done
	p.mu.Unlock()
	return d
}

// Started returns a channel that is closed when the process is started
func (p *Process) Started() <-chan struct{} {
	p.mu.Lock()
	// We create this here in case this is called before Start()
	if p.started == nil {
		p.started = make(chan struct{})
	}
	d := p.started
	p.mu.Unlock()
	return d
}

// Kill terminates the process gracefully. Initially a SIGTERM is sent, and
// then 10 seconds later a SIGTERM is sent.
func (p *Process) Kill() error {
	var err error
	if runtime.GOOS == "windows" {
		// Sending Interrupt on Windows is not implemented.
		// https://golang.org/src/os/exec.go?s=3842:3884#L110
		err = exec.Command("CMD", "/C", "TASKKILL", "/F", "/T", "/PID", strconv.Itoa(p.Pid)).Run()
	} else {
		// Send a sigterm
		err = p.signal(syscall.SIGTERM)
	}
	if err != nil {
		return err
	}

	select {
	// Was successfully terminated
	case <-p.Done():
		logger.Debug("[Process] Process with PID: %d has exited.", p.Pid)
		return nil

	// Forcefully kill the process after 10 seconds
	case <-time.After(10 * time.Second):
		logger.Debug("[Process] Process %d didn't terminate within 10 seconds, killing.", p.Pid)
		return p.signal(syscall.SIGKILL)
	}
}

func (p *Process) signal(sig os.Signal) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.command != nil && p.command.Process != nil {
		logger.Debug("[Process] Sending signal: %s to PID: %d", sig.String(), p.Pid)

		err := p.command.Process.Signal(sig)
		if err != nil {
			logger.Error("[Process] Failed to send signal: %s to PID: %d (%T: %v)", sig.String(), p.Pid, err, err)
			return err
		}
	} else {
		logger.Debug("[Process] No process to signal yet")
	}

	return nil
}

// https://github.com/hnakamur/commango/blob/fe42b1cf82bf536ce7e24dceaef6656002e03743/os/executil/executil.go#L29
// TODO: Can this be better?
func getExitStatus(waitResult error) string {
	exitStatus := -1

	if waitResult != nil {
		if err, ok := waitResult.(*exec.ExitError); ok {
			if s, ok := err.Sys().(syscall.WaitStatus); ok {
				exitStatus = s.ExitStatus()
			} else {
				logger.Error("[Process] Unimplemented for system where exec.ExitError.Sys() is not syscall.WaitStatus.")
			}
		} else {
			logger.Error("[Process] Unexpected error type in getExitStatus: %#v", waitResult)
		}
	} else {
		exitStatus = 0
	}

	return fmt.Sprintf("%d", exitStatus)
}

func timeoutWait(waitGroup *sync.WaitGroup) error {
	// Make a chanel that we'll use as a timeout
	c := make(chan int, 1)

	// Start waiting for the routines to finish
	go func() {
		waitGroup.Wait()
		c <- 1
	}()

	select {
	case _ = <-c:
		return nil
	case <-time.After(10 * time.Second):
		return errors.New("Timeout")
	}
}
