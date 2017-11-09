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
	"github.com/mattn/go-shellwords"
)

type Process struct {
	Pid        int
	PTY        bool
	Timestamp  bool
	Script     string
	Env        []string
	ExitStatus string

	buffer bytes.Buffer

	command *exec.Cmd

	// This callback is called when the process offically starts
	StartCallback func()

	// For every line in the process output, this callback will be called
	// with the contents of the line if its filter returns true.
	LineCallback       func(string)
	LinePreProcessor   func(string) string
	LineCallbackFilter func(string) bool

	// Running is stored as an int32 so we can use atomic operations to
	// set/get it (it's accessed by multiple goroutines)
	running int32
}

// If you change header parsing here make sure to change it in the
// buildkite.com frontend logic, too

var headerExpansionRegex = regexp.MustCompile("^(?:\\^\\^\\^\\s+\\+\\+\\+)$")

func (p *Process) Start() error {
	args, err := shellwords.Parse(p.Script)
	if err != nil {
		return err
	}

	p.command = exec.Command(args[0], args[1:]...)

	// Copy the current processes ENV and merge in the new ones. We do this
	// so the sub process gets PATH and stuff. We merge our path in over
	// the top of the current one so the ENV from Buildkite and the agent
	// take precedence over the agent
	currentEnv := os.Environ()
	p.command.Env = append(currentEnv, p.Env...)

	var waitGroup sync.WaitGroup

	lineReaderPipe, lineWriterPipe := io.Pipe()

	var multiWriter io.Writer
	if p.Timestamp {
		multiWriter = io.MultiWriter(lineWriterPipe)
	} else {
		multiWriter = io.MultiWriter(&p.buffer, lineWriterPipe)
	}

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

	// Add the line callback routine to the waitGroup
	waitGroup.Add(1)

	go func() {
		logger.Debug("[LineScanner] Starting to read lines")

		reader := bufio.NewReader(lineReaderPipe)

		var appending []byte
		var lineCallbackWaitGroup sync.WaitGroup

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
				// bufio.ReadLine returns a slice which is only valid until the next invocation
				// since it points to its own internal buffer array. To accumulate the entire
				// result we make a copy of the first prefix, and insure there is spare capacity
				// for future appends to minimize the need for resizing on append.
				appending = make([]byte, len(line), (cap(line))*2)
				copy(appending, line)

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

			// If we're timestamping this main thread will take
			// the hit of running the regex so we can build up
			// the timestamped buffer without breaking headers,
			// otherwise we let the goroutines take the perf hit.

			checkedForCallback := false
			lineHasCallback := false
			lineString := p.LinePreProcessor(string(line))

			// Create the prefixed buffer
			if p.Timestamp {
				lineHasCallback = p.LineCallbackFilter(lineString)
				checkedForCallback = true
				if lineHasCallback || headerExpansionRegex.MatchString(lineString) {
					// Don't timestamp special lines (e.g. header)
					p.buffer.WriteString(fmt.Sprintf("%s\n", line))
				} else {
					currentTime := time.Now().UTC().Format(time.RFC3339)
					p.buffer.WriteString(fmt.Sprintf("[%s] %s\n", currentTime, line))
				}
			}

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

		// We need to make sure all the line callbacks have finish before
		// finish up the process
		logger.Debug("[LineScanner] Waiting for callbacks to finish")
		lineCallbackWaitGroup.Wait()

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
	p.setRunning(false)

	// Find the exit status of the script
	p.ExitStatus = getExitStatus(waitResult)

	logger.Info("Process with PID: %d finished with Exit Status: %s", p.Pid, p.ExitStatus)

	// Sometimes (in docker containers) io.Copy never seems to finish. This is a mega
	// hack around it. If it doesn't finish after 1 second, just continue.
	logger.Debug("[Process] Waiting for routines to finish")
	err = timeoutWait(&waitGroup)
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
