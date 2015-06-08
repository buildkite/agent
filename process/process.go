package process

// Logic for this file is largely based on:
// https://github.com/jarib/childprocess/blob/783f7a00a1678b5d929062564ef5ae76822dfd62/lib/childprocess/unix/process.rb

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"github.com/buildkite/agent/logger"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type Process struct {
	Pid           int
	Running       bool
	PTY           bool
	Script        string
	Env           []string
	ExitStatus    string
	buffer        bytes.Buffer
	command       *exec.Cmd
	StartCallback func()
	LineCallback  func(string)
}

func (p Process) Create() *Process {
	// Find the script to run
	absolutePath, _ := filepath.Abs(p.Script)
	scriptDirectory := filepath.Dir(absolutePath)

	// Create the command that will be run
	p.command = exec.Command(absolutePath)
	p.command.Dir = scriptDirectory

	// Do cross-platform things to prepare this process to run
	PrepareCommandProcess(&p)

	// Copy the current processes ENV and merge in the new ones. We do this
	// so the sub process gets PATH and stuff. We merge our path in over
	// the top of the current one so the ENV from Buildkite and the agent
	// take precedence over the agent
	currentEnv := os.Environ()
	p.command.Env = append(currentEnv, p.Env...)

	return &p
}

func (p *Process) Start() error {
	var waitGroup sync.WaitGroup

	lineReaderPipe, lineWriterPipe := io.Pipe()

	multiWriter := io.MultiWriter(&p.buffer, lineWriterPipe)

	logger.Info("Starting to run script: %s", p.command.Path)

	// Toggle between running in a pty
	if p.PTY {
		pty, err := StartPTY(p.command)
		if err != nil {
			p.ExitStatus = "1"
			return err
		}

		p.Pid = p.command.Process.Pid
		p.Running = true

		waitGroup.Add(1)

		go func() {
			logger.Debug("[Process] Starting to copy PTY to the buffer")

			// Copy the pty to our buffer. This will block until it
			// EOF's or something breaks.
			_, err = io.Copy(multiWriter, pty)
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
		p.command.Stdout = multiWriter
		p.command.Stderr = multiWriter

		err := p.command.Start()
		if err != nil {
			p.ExitStatus = "1"
			return err
		}

		p.Pid = p.command.Process.Pid
		p.Running = true
	}

	logger.Info("[Process] Process is running with PID: %d", p.Pid)

	// Add the line callback routine to the waitGroup
	waitGroup.Add(1)

	go func() {
		logger.Debug("[LineScanner] Starting to read lines")

		reader := bufio.NewReader(lineReaderPipe)

		var appending []byte

		for {
			line, isPrefix, err := reader.ReadLine()
			if err != nil {
				if err == io.EOF {
					logger.Debug("[LineScanner] Encountered EOF")
					break
				}

				logger.Error("[LineScanner] Failed to read: (%T: %v)", err, err)
			}

			// If isPrefix is true, that means we've got a really
			// long line incoming, and we'll keep appending to it
			// until isPrefix is false (which means the long line
			// has ended.
			if isPrefix && appending == nil {
				logger.Debug("[LineScanner] Line is too long to read, going to buffer it until it finishes")
				appending = line

				continue
			}

			// Should we be appending?
			if appending != nil {
				appending = append(appending, line...)

				// No more isPrefix! Line is finished!
				if !isPrefix {
					logger.Debug("[LineScanner] Finished buffering long line")
					line = appending

					// Reset appending back to nil
					appending = nil
				} else {
					continue
				}
			}

			go p.LineCallback(string(line))
		}

		logger.Debug("[LineScanner] Finished")

		waitGroup.Done()
	}()

	// Call the StartCallback
	go p.StartCallback()

	// Wait until the process has finished. The returned error is nil if the command runs,
	// has no problems copying stdin, stdout, and stderr, and exits with a zero exit status.
	waitResult := p.command.Wait()

	// Close the line writer pipe
	lineWriterPipe.Close()

	// The process is no longer running at this point
	p.Running = false

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

func (p *Process) Output() string {
	return p.buffer.String()
}

func (p *Process) Kill() error {
	// Send a sigterm
	err := p.signal(syscall.SIGTERM)
	if err != nil {
		return err
	}

	// Make a channel that we'll use as a timeout
	c := make(chan int, 1)
	checking := true

	// Start a routine that checks to see if the process
	// is still alive.
	go func() {
		for checking {
			logger.Debug("[Process] Checking to see if PID: %d is still alive", p.Pid)

			foundProcess, err := os.FindProcess(p.Pid)

			// Can't find the process at all
			if err != nil {
				logger.Debug("[Process] Could not find process with PID: %d", p.Pid)

				break
			}

			// We have some information about the process
			if foundProcess != nil {
				processState, err := foundProcess.Wait()

				if err != nil || processState.Exited() {
					logger.Debug("[Process] Process with PID: %d has exited.", p.Pid)

					break
				}
			}

			// Retry in a moment
			sleepTime := time.Duration(1 * time.Second)
			time.Sleep(sleepTime)
		}

		c <- 1
	}()

	// Timeout this process after 3 seconds
	select {
	case _ = <-c:
		// Was successfully terminated
	case <-time.After(10 * time.Second):
		// Stop checking in the routine above
		checking = false

		// Forcefully kill the thing
		err = p.signal(syscall.SIGKILL)

		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Process) signal(sig os.Signal) error {
	logger.Debug("[Process] Sending signal: %s to PID: %d", sig.String(), p.Pid)

	err := p.command.Process.Signal(syscall.SIGTERM)
	if err != nil {
		logger.Error("[Process] Failed to send signal: %s to PID: %d (%T: %v)", sig.String(), p.Pid, err, err)
		return err
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

	return nil
}
