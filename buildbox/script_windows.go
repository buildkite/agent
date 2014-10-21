package buildbox

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type Process struct {
	Output     string
	Pid        int
	Running    bool
	RunInPty   bool
	ExitStatus string
	command    *exec.Cmd
	callback   func(*Process)
}

// Implement the Stringer thingy
func (p Process) String() string {
	return fmt.Sprintf("Process{Pid: %d, Running: %t, ExitStatus: %s}", p.Pid, p.Running, p.ExitStatus)
}

func InitProcess(scriptPath string, env []string, runInPty bool, callback func(*Process)) *Process {
	// Create a new instance of our process struct
	var process Process
	process.RunInPty = runInPty

	// Find the script to run
	absolutePath, _ := filepath.Abs(scriptPath)
	scriptDirectory := filepath.Dir(absolutePath)

	process.command = exec.Command(absolutePath)
	process.command.Dir = scriptDirectory

	// Copy the current processes ENV and merge in the new ones. We do this
	// so the sub process gets PATH and stuff.
	currentEnv := os.Environ()
	process.command.Env = append(currentEnv, env...)

	// Set the callback
	process.callback = callback

	return &process
}

func (p *Process) Start() error {
	var buffer bytes.Buffer
	var waitGroup sync.WaitGroup

	Logger.Infof("Starting to run script: %s", p.command.Path)

	p.command.Stdout = &buffer
	p.command.Stderr = &buffer

	err := p.command.Start()
	if err != nil {
		p.ExitStatus = "1"
		return err
	}

	p.Pid = p.command.Process.Pid
	p.Running = true

	// We only have to wait for 1 thing if we're not running in a PTY.
	waitGroup.Add(1)

	Logger.Infof("Process is running with PID: %d", p.Pid)

	go func() {
		for p.Running {
			Logger.Debug("Copying buffer to the process output")

			// Convert the stdout buffer to a string
			p.Output = buffer.String()

			// Call the callback and pass in our process object
			p.callback(p)

			// Sleep for 1 second
			time.Sleep(1000 * time.Millisecond)
		}

		Logger.Debug("Finished routine that copies the buffer to the process output")

		waitGroup.Done()
	}()

	// Wait until the process has finished. The returned error is nil if the command runs,
	// has no problems copying stdin, stdout, and stderr, and exits with a zero exit status.
	waitResult := p.command.Wait()

	// The process is no longer running at this point
	p.Running = false

	// Find the exit status of the script
	p.ExitStatus = getExitStatus(waitResult)

	Logger.Infof("Process with PID: %d finished with Exit Status: %s", p.Pid, p.ExitStatus)

	// Sometimes (in docker containers) io.Copy never seems to finish. This is a mega
	// hack around it. If it doesn't finish after 1 second, just continue.
	Logger.Debug("Waiting for io.Copy and incremental output to finish")
	err = timeoutWait(&waitGroup)
	if err != nil {
		Logger.Errorf("Timed out waiting for wait group: (%T: %v)", err, err)
	}

	// Copy the final output back to the process
	p.Output = buffer.String()

	// No error occured so we can return nil
	return nil
}

func (p *Process) Kill() error {
	// Send a sigterm
	err := p.signal(syscall.SIGTERM)
	if err != nil {
		return err
	}

	// Make a chanel that we'll use as a timeout
	c := make(chan int, 1)
	checking := true

	// Start a routine that checks to see if the process
	// is still alive.
	go func() {
		for checking {
			Logger.Debugf("Checking to see if PID: %d is still alive", p.Pid)

			foundProcess, err := os.FindProcess(p.Pid)

			// Can't find the process at all
			if err != nil {
				Logger.Debugf("Could not find process with PID: %d", p.Pid)

				break
			}

			// We have some information about the procss
			if foundProcess != nil {
				processState, err := foundProcess.Wait()

				if err != nil || processState.Exited() {
					Logger.Debugf("Process with PID: %d has exited.", p.Pid)

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
	Logger.Debugf("Sending signal: %s to PID: %d", sig.String(), p.Pid)

	err := p.command.Process.Signal(syscall.SIGTERM)
	if err != nil {
		Logger.Errorf("Failed to send signal: %s to PID: %d (%T: %v)", sig.String(), p.Pid, err, err)
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
				Logger.Error("Unimplemented for system where exec.ExitError.Sys() is not syscall.WaitStatus.")
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
	case <-time.After(5 * time.Second):
		return errors.New("Timeout")
	}

	return nil
}
