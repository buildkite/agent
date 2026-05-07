// Package process provides a helper for running and managing a subprocess.
//
// It is intended for internal use by buildkite-agent only.
package process

import (
	"cmp"
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
)

const (
	termType = "xterm-256color"
)

// afterPTYStartHook lets tests force work to happen after the PTY helper
// returns so they can verify raw-mode ordering around process startup.
var afterPTYStartHook = func() {}

type Signal int

const (
	SIGHUP  Signal = 1
	SIGINT  Signal = 2
	SIGQUIT Signal = 3
	SIGKILL Signal = 9
	SIGUSR1 Signal = 10
	SIGUSR2 Signal = 12
	SIGTERM Signal = 15
)

var signalMap = map[string]Signal{
	"SIGHUP":  SIGHUP,
	"SIGINT":  SIGINT,
	"SIGQUIT": SIGQUIT,
	"SIGKILL": SIGKILL,
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
	Started           chan struct{}
	Done              chan struct{}
}

// Process is an operating system level process
type Process struct {
	waitResult error
	status     syscall.WaitStatus
	conf       Config
	logger     logger.Logger

	mu            sync.Mutex
	command       *exec.Cmd
	started, done chan struct{}

	winJobHandle  uintptr      //nolint:unused // Used in signal_windows.go
	terminateFunc func() error //nolint:unused // Used in signal_windows.go
}

// New returns a new instance of Process
func New(l logger.Logger, c Config) *Process {
	return &Process{
		logger:  l,
		conf:    c,
		started: c.Started,
		done:    c.Done,
	}
}

// Pid is the pid of the running process, or 0 if it is not running.
func (p *Process) Pid() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.command == nil || p.command.Process == nil {
		return 0
	}
	return p.pid()
}

// unguarded version of Pid, only safe to call after process has started
func (p *Process) pid() int { return p.command.Process.Pid }

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
	// Setup and start the command.
	cleanup, err := p.start(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	p.logger.Infof("[Process] Process is running with PID: %d", p.pid())

	// Wait until the process has finished. The returned error is nil if the
	// command runs, has no problems copying stdin, stdout, and stderr, and
	// exits with a zero exit status.
	waitResult := p.command.Wait()

	// Log and record the result
	return p.complete(waitResult)
}

// start sets up the command and starts it, then closes p.started. It returns a
// cleanup function.
func (p *Process) start(ctx context.Context) (func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Create the command, etc
	if err := p.setup(ctx); err != nil {
		return nil, err
	}

	// Run in a PTY or not?
	start := p.startWithoutPTY
	if p.conf.PTY {
		start = p.startWithPTY
	}

	// Start!
	cleanup, err := start(ctx)
	if err != nil {
		return nil, err
	}

	if err := p.postStart(); err != nil {
		p.logger.Errorf("[Process] postStart failed: %v", err)
	}
	// Signal waiting consumers in Started() by closing the started channel
	close(p.started)
	return cleanup, nil
}

// setup sets up the command. It should be called under p.mu.
func (p *Process) setup(ctx context.Context) error {
	if p.command != nil {
		return errors.New("process is already running")
	}

	// Create a command. Upon context cancellation, call p.onContextCancel,
	// which calls p.Interrupt, and if the process continues past the signal
	// grace period, p.Terminate.
	p.command = exec.CommandContext(ctx, p.conf.Path, p.conf.Args...)
	p.command.Cancel = p.onContextCancel

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

	// Ensure channels for signalling started and done are not nil
	p.done = cmp.Or(p.done, make(chan struct{}))
	p.started = cmp.Or(p.started, make(chan struct{}))

	// Copy the current processes ENV and merge in the new ones. We do this
	// so the sub process gets PATH and stuff. We merge our path in over
	// the top of the current one so the ENV from Buildkite and the agent
	// take precedence over the agent
	currentEnv := os.Environ()
	p.command.Env = append(currentEnv, p.conf.Env...)
	return nil
}

// startWithPTY starts the process in a PTY, and a goroutine for copying the PTY
// to stdout. The cleanup function waits for the copy to finish and closes the
// PTY handle.
func (p *Process) startWithPTY(ctx context.Context) (func(), error) {
	p.logger.Debugf("[Process] Running with a PTY")

	// Commands like tput expect a TERM value for a PTY
	p.command.Env = append(p.command.Env, "TERM="+termType)

	rawPTY := experiments.IsEnabled(ctx, experiments.PTYRaw)

	pty, err := StartPTY(p.command, rawPTY)
	if err != nil {
		return nil, fmt.Errorf("error starting pty: %w", err)
	}

	afterPTYStartHook()

	if rawPTY {
		p.logger.Debugf("[Process] Setting raw mode for PTY %s (fd:%d)", pty.Name(), pty.Fd())
	}

	// Copy and close the PTY, if it exists.
	copyDone := make(chan struct{})
	go p.copyPTYToStdout(pty, copyDone)

	return func() {
		// Sometimes (in docker containers) io.Copy never seems to finish. This is a
		// mega hack around it. If it doesn't finish after 10 seconds, just continue.
		p.logger.Debugf("[Process] Waiting for routines to finish")

		select {
		case <-time.After(10 * time.Second):
			p.logger.Debugf("[Process] Timed out waiting for PTY->stdout copy")
		case <-copyDone:
			// it's done
		}

		pty.Close() //nolint:errcheck // Best-effort cleanup
	}, nil
}

// startWithoutPTY starts the process without using a PTY. The cleanup function
// is a no-op.
func (p *Process) startWithoutPTY(context.Context) (func(), error) {
	p.logger.Debugf("[Process] Running without a PTY")

	p.command.Stdin = p.conf.Stdin
	p.command.Stdout = p.conf.Stdout
	p.command.Stderr = p.conf.Stderr

	if err := p.command.Start(); err != nil {
		return nil, fmt.Errorf("error starting command: %w", err)
	}
	return func() {}, nil
}

// copyPTYToStdout copies pty to p.conf.Stdout. It should be a new goroutine.
func (p *Process) copyPTYToStdout(pty *os.File, copyDone chan<- struct{}) {
	defer close(copyDone)
	p.logger.Debugf("[Process] Starting to copy PTY to the buffer")

	// Copy the pty to our writer. This will block until it EOFs or something breaks.
	_, err := io.Copy(p.conf.Stdout, pty)
	switch {
	case errors.Is(err, syscall.EIO):
		// We can safely ignore syscall.EIO errors, at least on linux
		// because it's just the PTY telling us that it closed
		// successfully.
		//
		// See: https://github.com/buildkite/agent/pull/34#issuecomment-46080419
		//
		// We will still log the error to aid debugging on other platforms.
		p.logger.Debugf("[Process] PTY has finished being copied to the buffer: %v", err)

	case err == nil:
		p.logger.Debugf("[Process] PTY has finished being copied to the buffer")

	default:
		p.logger.Errorf("[Process] PTY output copy failed with error: %T: %v", err, err)
	}
}

// complete stores the waitResult, wait status, logs completion, and closes
// p.done.
func (p *Process) complete(waitResult error) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.waitResult = waitResult
	// Signal waiting consumers in Done() by closing the done channel
	close(p.done)

	// Convert the wait result into a native WaitStatus
	if waitResult != nil {
		var exitErr *exec.ExitError
		if !errors.As(waitResult, &exitErr) {
			return fmt.Errorf("unexpected waitResult error type %[1]T: %[1]w", waitResult)
		}

		waitStatus, isWS := exitErr.Sys().(syscall.WaitStatus)
		if !isWS {
			return ErrNotWaitStatus
		}
		p.status = waitStatus
	}

	// Find the exit status or terminating signal of the script
	exitSignal := "nil"
	if p.status.Signaled() {
		exitSignal = SignalString(p.status.Signal())
	}
	p.logger.Infof("Process with PID: %d finished with Exit Status: %d, Signal: %s",
		p.pid(), p.status.ExitStatus(), exitSignal)
	return nil
}

// onContextCancel interrupts the process, waits for the process to exit for the
// signal grace period, then terminates the process. It is called by the
// Command.Cancel mechanism when the context for p.command is cancelled.
func (p *Process) onContextCancel() error {
	p.logger.Debugf("[Process] Context done, terminating. pid=%d", p.pid())
	if err := p.Interrupt(); err != nil {
		p.logger.Warnf("[Process] Failed termination: %v", err)
	}

	// We could almost use Command.WaitDelay to implement the signal grace
	// period, but we want to kill *all* child processes, not just the primary
	// process. exec.Command seems to lack a way to override its behaviour for
	// when WaitDelay is reached. Hence, we have a separate goroutine:
	go func() {
		// Wait for signal grace period or the process to be done
		select {
		case <-p.Done(): // process exited after being interrupted above
			return
		case <-time.After(p.conf.SignalGracePeriod):
			// continue below
		}

		p.logger.Warnf("[Process] Has not terminated in time, killing. pid=%d", p.pid())
		if err := p.Terminate(); err != nil {
			// Oh Well, At Least We Tried™
			p.logger.Errorf("[Process] error sending SIGKILL: %s", err)
		}
	}()

	// Suppress "context canceled" by returning this.
	return os.ErrProcessDone
}

// Done returns a channel that is closed when the process finishes
func (p *Process) Done() <-chan struct{} {
	p.mu.Lock()
	// We create this here in case this is called before Start()
	p.done = cmp.Or(p.done, make(chan struct{}))
	d := p.done
	p.mu.Unlock()
	return d
}

// Started returns a channel that is closed when the process is started
func (p *Process) Started() <-chan struct{} {
	p.mu.Lock()
	// We create this here in case this is called before Start()
	p.started = cmp.Or(p.started, make(chan struct{}))
	d := p.started
	p.mu.Unlock()
	return d
}

// Interrupt interrupts the process on platforms that support it, and terminates
// otherwise.
func (p *Process) Interrupt() error {
	if p == nil {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.command == nil || p.command.Process == nil {
		p.logger.Debugf("[Process] No process to interrupt yet")
		return nil
	}

	// interrupt the process ~~(ctrl-c or SIGINT)~~
	// Actually, this sends SIGTERM (*nixes) or Ctrl-Break (Windows).
	// One day we might change that to SIGINT/Ctrl-C.
	if err := p.interruptProcessGroup(); err != nil {
		//  No process or process group can be found corresponding to that specified by pid.
		if errors.Is(err, syscall.ESRCH) {
			p.logger.Warnf("[Process] Process %d has already exited", p.pid())
			return nil
		}

		p.logger.Errorf("[Process] Failed to interrupt process %d: %v", p.pid(), err)

		// Fallback to terminating if we get an error
		return p.terminateProcessGroup()
	}

	return nil
}

// Terminate terminates (kills) the process.
func (p *Process) Terminate() error {
	if p == nil {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.command == nil || p.command.Process == nil {
		p.logger.Debugf("[Process] No process to terminate yet")
		return nil
	}

	return p.terminateProcessGroup()
}
