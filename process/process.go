package process

// Logic for this file is largely based on:
// https://github.com/jarib/childprocess/blob/783f7a00a1678b5d929062564ef5ae76822dfd62/lib/childprocess/unix/process.rb

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
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

	// StartCallback is called when the process is started
	StartCallback func()

	// LinePreProcessor is called with ever line of output and can be used to modify each line
	// The modified value is then made available to LineCallbackFilter and LineCallback
	LinePreProcessor func(string) string

	// LineCallbackFilter will be called for every line, determines if LineCallback should be called
	LineCallbackFilter func(string) bool

	// LineCallback is an asynchronous call back that will be dispatched for every line, provided
	// LineCallbackFilter returned true.
	LineCallback func(string)

	buffer  outputBuffer
	command *exec.Cmd

	// Running is stored as an int32 so we can use atomic operations to
	// set/get it (it's accessed by multiple goroutines)
	running int32
}

// If you change header parsing here make sure to change it in the
// buildkite.com frontend logic, too

var headerExpansionRegex = regexp.MustCompile("^(?:\\^\\^\\^\\s+\\+\\+\\+)\\s*$")

func (p *Process) Start() error {
	p.command = exec.Command(p.Script[0], p.Script[1:]...)

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
		p.setRunning(true)

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
		p.setRunning(true)
	}

	logger.Info("[Process] Process is running with PID: %d", p.Pid)

	scanner := bufio.NewScanner(lineReaderPipe)

	var lineCallbackWaitGroup sync.WaitGroup

	// Add the line callback routine to the waitGroup
	waitGroup.Add(1)

	go func() {
		defer waitGroup.Done()

		// We scan line by line so that we can run our various processors, currently this buffers the entire
		// output in memory and then an asynchronous process reads it in chunks
		logger.Debug("[LineScanner] Starting to read lines")
		for scanner.Scan() {
			line := scanner.Text()

			checkedForCallback := false
			lineHasCallback := false
			lineString := p.LinePreProcessor(line)

			// Optionally prefix lines with timestamps
			if p.Timestamp {
				lineHasCallback = p.LineCallbackFilter(lineString)
				checkedForCallback = true

				// Don't timestamp special lines (e.g. header)
				// FIXME: this should be moved to agent/job_runner.go
				if lineHasCallback || headerExpansionRegex.MatchString(lineString) {
					_, _ = p.buffer.WriteString(fmt.Sprintf("%s\n", line))
				} else {
					currentTime := time.Now().UTC().Format(time.RFC3339)
					_, _ = p.buffer.WriteString(fmt.Sprintf("[%s] %s\n", currentTime, line))
				}
			} else {
				_, _ = p.buffer.WriteString(line + "\n")
			}

			// A callback is an async function that is triggered by a line
			if lineHasCallback || !checkedForCallback {
				lineCallbackWaitGroup.Add(1)
				go func(line string) {
					defer lineCallbackWaitGroup.Done()
					if (checkedForCallback && lineHasCallback) || p.LineCallbackFilter(lineString) {
						p.LineCallback(line)
					}
				}(lineString)
			}
		}

		if err := scanner.Err(); err != nil {
			logger.Debug("[LineScanner] Error from scanner: %v", err)
		}

		// We need to make sure all the line callbacks have finish before
		// finish up the process
		logger.Debug("[LineScanner] Waiting for callbacks to finish")
		lineCallbackWaitGroup.Wait()

		logger.Debug("[LineScanner] Finished")
	}()

	// Call the StartCallback
	go p.StartCallback()

	// Wait until the process has finished. The returned error is nil if the command runs,
	// has no problems copying stdin, stdout, and stderr, and exits with a zero exit status.
	waitResult := p.command.Wait()

	// Close the line writer pipe
	_ = lineWriterPipe.Close()

	// The process is no longer running at this point
	p.setRunning(false)

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
	var err error
	if runtime.GOOS == "windows" {
		// Sending Interrupt on Windows is not implemented.
		// https://golang.org/src/os/exec.go?s=3842:3884#L110
		err = exec.Command("CMD", "/C", "TASKKILL", "/F", "/PID", strconv.Itoa(p.Pid)).Run()
	} else {
		// Send a sigterm
		err = p.signal(syscall.SIGTERM)
	}
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

// Returns whether or not the process is running
func (p *Process) IsRunning() bool {
	return atomic.LoadInt32(&p.running) != 0
}

// Sets the running flag of the process
func (p *Process) setRunning(r bool) {
	// Use the atomic package to avoid race conditions when setting the
	// `running` value from multiple routines
	if r {
		atomic.StoreInt32(&p.running, 1)
	} else {
		atomic.StoreInt32(&p.running, 0)
	}
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

	return nil
}

// outputBuffer is a goroutine safe bytes.Buffer
type outputBuffer struct {
	sync.RWMutex
	buf bytes.Buffer
}

// Write appends the contents of p to the buffer, growing the buffer as needed. It returns
// the number of bytes written.
func (ob *outputBuffer) Write(p []byte) (n int, err error) {
	ob.Lock()
	defer ob.Unlock()
	return ob.buf.Write(p)
}

// WriteString appends the contents of s to the buffer, growing the buffer as needed. It returns
// the number of bytes written.
func (ob *outputBuffer) WriteString(s string) (n int, err error) {
	return ob.Write([]byte(s))
}

// String returns the contents of the unread portion of the buffer
// as a string.  If the Buffer is a nil pointer, it returns "<nil>".
func (ob *outputBuffer) String() string {
	ob.RLock()
	defer ob.RUnlock()
	return ob.buf.String()
}
