// Package process provides a helper for running and managing a subprocess.
//
// It is intended for internal use by buildkite-agent only.
package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/logger"
	"golang.org/x/term"
)

const (
	termType = "xterm-256color"
)

type Signal int

const (
	SIGHUP  Signal = 1
	SIGINT  Signal = 2
	SIGQUIT Signal = 3
	SIGUSR1 Signal = 10
	SIGUSR2 Signal = 12
	SIGTERM Signal = 15
)

var signalMap = map[string]Signal{
	"SIGHUP":  SIGHUP,
	"SIGINT":  SIGINT,
	"SIGQUIT": SIGQUIT,
	"SIGUSR1": SIGUSR1,
	"SIGUSR2": SIGUSR2,
	"SIGTERM": SIGTERM,
}

var ErrNotWaitStatus = errors.New(
	"unimplemented for systems where exec.ExitError.Sys() is not syscall.WaitStatus",
)

type WaitStatus interface {
	ExitStatus() int
	Signaled() bool
	Signal() syscall.Signal
}

func (s Signal) String() string {
	for k, sig := range signalMap {
		if sig == s {
			return k
		}
	}
	return strconv.FormatInt(int64(s), 10)
}

func ParseSignal(sig string) (Signal, error) {
	s, ok := signalMap[strings.ToUpper(sig)]
	if !ok {
		return Signal(0), fmt.Errorf("unknown signal %q", sig)
	}
	return s, nil
}

// Configuration for a Process
type Config struct {
	PTY               bool
	Path              string
	Args              []string
	Env               []string
	Stdin             io.Reader
	Stdout            io.Writer
	Stderr            io.Writer
	Dir               string
	InterruptSignal   Signal
	SignalGracePeriod time.Duration
}

// Process is an operating system level process
type Process struct {
	waitResult error
	status     syscall.WaitStatus
	conf       Config
	logger     logger.Logger
	command    *exec.Cmd

	mu            sync.Mutex
	pid           int
	started, done chan struct{}

	winJobHandle uintptr //nolint:unused // Used in signal_windows.go
}

// New returns a new instance of Process
func New(l logger.Logger, c Config) *Process {
	return &Process{
		logger: l,
		conf:   c,
	}
}

// Pid is the pid of the running process
func (p *Process) Pid() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pid
}

// WaitResult returns the raw error returned by Wait()
func (p *Process) WaitResult() error {
	return p.waitResult
}

// WaitStatus returns the status of the Wait() call
func (p *Process) WaitStatus() WaitStatus {
	return p.status
}

// Run the command and block until it finishes
func (p *Process) Run(ctx context.Context) error {
	if p.command != nil {
		return fmt.Errorf("process is already running")
	}

	// Create a command
	p.command = exec.Command(p.conf.Path, p.conf.Args...)

	// Setup the process to create a process group if supported
	p.setupProcessGroup()

	// Configure working dir and fail if it doesn't exist, otherwise
	// we get confusing errors about fork/exec failing because the file
	// doesn't exist
	if p.conf.Dir != "" {
		if _, err := os.Stat(p.conf.Dir); os.IsNotExist(err) {
			return fmt.Errorf("process working directory %q doesn't exist", p.conf.Dir)
		}
		p.command.Dir = p.conf.Dir
	}

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
	p.command.Env = append(currentEnv, p.conf.Env...)

	var waitGroup sync.WaitGroup

	// Toggle between running in a pty
	if p.conf.PTY {
		p.logger.Debug("[Process] Running with a PTY")

		// Commands like tput expect a TERM value for a PTY
		p.command.Env = append(p.command.Env, "TERM="+termType)

		pty, err := StartPTY(p.command)
		if err != nil {
			return fmt.Errorf("error starting pty: %w", err)
		}

		// Make sure to close the pty at the end.
		defer func() { _ = pty.Close() }()

		if experiments.IsEnabled(ctx, experiments.PTYRaw) {
			p.logger.Debug("[Process] Setting raw mode for PTY %s (fd:%d)", pty.Name(), pty.Fd())
			// No need to capture/restore old state, because we close the PTY when we're done.
			_, err = term.MakeRaw(int(pty.Fd()))
			if err != nil {
				return fmt.Errorf("error putting PTY into raw mode: %w", err)
			}
		}

		p.mu.Lock()
		p.pid = p.command.Process.Pid
		p.mu.Unlock()

		// Signal waiting consumers in Started() by closing the started channel
		close(p.started)

		waitGroup.Add(1)

		go func() {
			p.logger.Debug("[Process] Starting to copy PTY to the buffer")

			// Copy the pty to our writer. This will block until it EOFs or something breaks.
			_, err = io.Copy(p.conf.Stdout, pty)
			switch {
			case errors.Is(err, syscall.EIO):
				// We can safely ignore syscall.EIO errors, at least on linux
				// because it's just the PTY telling us that it closed
				// successfully.
				//
				// See: https://github.com/buildkite/agent/pull/34#issuecomment-46080419
				//
				// We will still log the error to aid debugging on other platforms.
				p.logger.Debug("[Process] PTY has finished being copied to the buffer: %v", err)

			case err == nil:
				p.logger.Debug("[Process] PTY has finished being copied to the buffer")

			default:
				p.logger.Error("[Process] PTY output copy failed with error: %T: %v", err, err)
			}

			waitGroup.Done()
		}()
	} else {
		p.logger.Debug("[Process] Running without a PTY")

		p.command.Stdin = p.conf.Stdin
		p.command.Stdout = p.conf.Stdout
		p.command.Stderr = p.conf.Stderr

		if err := p.command.Start(); err != nil {
			return fmt.Errorf("error starting command: %w", err)
		}

		if err := p.postStart(); err != nil {
			p.logger.Error("[Process] postStart failed: %v", err)
		}

		p.mu.Lock()
		p.pid = p.command.Process.Pid
		p.mu.Unlock()

		// Signal waiting consumers in Started() by closing the started channel
		close(p.started)
	}

	// When the context finishes, terminate the process
	go func() {
		if ctx == nil {
			return
		}

		select {
		case <-p.Done(): // process exited of it's own accord
			return
		case <-ctx.Done():
			p.logger.Debug("[Process] Context done, terminating. pid=%d", p.pid)
			if err := p.Interrupt(); err != nil {
				p.logger.Warn("[Process] Failed termination: %v", err)
			}

			select {
			case <-p.Done(): // process exited after being interrupted
				return
			case <-time.After(p.conf.SignalGracePeriod):
				p.logger.Warn("[Process] Has not terminated in time, killing. pid=%d", p.pid)
				if err := p.Terminate(); err != nil {
					p.logger.Error("[Process] error sending SIGKILL: %s", err)
					return
				}
			}
		}
	}()

	p.logger.Info("[Process] Process is running with PID: %d", p.pid)

	// Wait until the process has finished. The returned error is nil if the
	// command runs, has no problems copying stdin, stdout, and stderr, and
	// exits with a zero exit status.
	p.waitResult = p.command.Wait()

	// Signal waiting consumers in Done() by closing the done channel
	close(p.done)

	// Convert the wait result into a native WaitStatus
	if p.waitResult != nil {
		exitErr := new(exec.ExitError)
		if !errors.As(p.waitResult, &exitErr) {
			return fmt.Errorf("unexpected error type %T: %w", p.waitResult, p.waitResult)
		}

		switch ws := exitErr.Sys().(type) {
		case syscall.WaitStatus: // posix
			p.status = ws
		default: // not-posix
			return ErrNotWaitStatus
		}
	}

	// Find the exit status or terminating signal of the script
	exitSignal := "nil"
	if p.status.Signaled() {
		exitSignal = SignalString(p.status.Signal())
	}
	p.logger.Info("Process with PID: %d finished with Exit Status: %d, Signal: %s",
		p.pid, p.status.ExitStatus(), exitSignal)

	// Sometimes (in docker containers) io.Copy never seems to finish. This is a mega
	// hack around it. If it doesn't finish after 1 second, just continue.
	p.logger.Debug("[Process] Waiting for routines to finish")
	err := timeoutWait(&waitGroup)
	if err != nil {
		p.logger.Debug("[Process] Timed out waiting for wait group: (%T: %v)", err, err)
	}

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

// Interrupt the process on platforms that support it, terminate otherwise
func (p *Process) Interrupt() error {
	if p == nil {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.command == nil || p.command.Process == nil {
		p.logger.Debug("[Process] No process to interrupt yet")
		return nil
	}

	// interrupt the process (ctrl-c or SIGINT)
	if err := p.interruptProcessGroup(); err != nil {
		//  No process or process group can be found corresponding to that specified by pid.
		if errors.Is(err, syscall.ESRCH) {
			p.logger.Warn("[Process] Process %d has already exited", p.pid)
			return nil
		}

		p.logger.Error("[Process] Failed to interrupt process %d: %v", p.pid, err)

		// Fallback to terminating if we get an error
		if termErr := p.terminateProcessGroup(); termErr != nil {
			return termErr
		}
	}

	return nil
}

// Terminate the process
func (p *Process) Terminate() error {
	if p == nil {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.command == nil || p.command.Process == nil {
		p.logger.Debug("[Process] No process to terminate yet")
		return nil
	}

	return p.terminateProcessGroup()
}

func timeoutWait(waitGroup *sync.WaitGroup) error {
	// Make a chanel that we'll use as a timeout
	ch := make(chan struct{})

	// Start waiting for the routines to finish
	go func() {
		waitGroup.Wait()
		close(ch)
	}()

	select {
	case <-ch:
		return nil
	case <-time.After(10 * time.Second):
		return errors.New("timeout")
	}
}
