// Package shell provides a cross-platform virtual shell abstraction for
// executing commands.
//
// It is intended for internal use by buildkite-agent only.
package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/opentracing/opentracing-go"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/experiments"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/process"
	"github.com/buildkite/agent/v3/shellscript"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/shellwords"
	"github.com/gofrs/flock"
	"github.com/nightlyone/lockfile"
)

var (
	lockRetryDuration = time.Second
)

// Shell represents a virtual shell, handles logging, executing commands and
// provides hooks for capturing output and exit conditions.
//
// Provides a lowest-common denominator abstraction over macOS, Linux and Windows
type Shell struct {
	Logger

	// The running environment for the shell
	Env *env.Environment

	// Whether the shell is a PTY
	PTY bool

	// stdin is an optional input stream used by Run() and friends.
	// It remains unexported on the assumption that it's not useful except via
	// WithStdin() to get a shell-copy prepared for a single command that needs
	// input.
	stdin io.Reader

	// Where stdout is written, defaults to os.Stdout
	Writer io.Writer

	// Whether to run the shell in debug mode
	Debug bool

	// Current working directory that shell commands get executed in
	wd string

	// Currently running command
	cmd     *command
	cmdLock sync.Mutex

	// The signal to use to interrupt the command
	InterruptSignal process.Signal
}

// New returns a new Shell
func New() (*Shell, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("Failed to find current working directory: %w", err)
	}

	return &Shell{
		Logger: StderrLogger,
		Env:    env.FromSlice(os.Environ()),
		Writer: os.Stdout,
		wd:     wd,
	}, nil
}

// WithStdin returns a copy of the Shell with the provided io.Reader set as the
// Stdin for the next command. The copy should be discarded after one command.
// For example, sh.WithStdin(strings.NewReader("hello world")).Run("cat")
func (s *Shell) WithStdin(r io.Reader) *Shell {
	// cargo-culted cmdLock, not sure if it's needed
	s.cmdLock.Lock()
	defer s.cmdLock.Unlock()
	// Can't copy struct like `newsh := *s` because sync.Mutex can't be copied.
	return &Shell{
		Logger:          s.Logger,
		Env:             s.Env,
		stdin:           r, // our new stdin
		Writer:          s.Writer,
		wd:              s.wd,
		InterruptSignal: s.InterruptSignal,
	}
}

// Getwd returns the current working directory of the shell
func (s *Shell) Getwd() string {
	return s.wd
}

// Chdir changes the working directory of the shell
func (s *Shell) Chdir(path string) error {
	// If the path isn't absolute, prefix it with the current working directory.
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.wd, path)
	}

	s.Promptf("cd %s", shellwords.Quote(path))

	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("Failed to change working: directory does not exist")
	}

	s.wd = path
	return nil
}

// AbsolutePath returns the absolute path to an executable based on the PATH and
// PATHEXT of the Shell
func (s *Shell) AbsolutePath(executable string) (string, error) {
	// Is the path already absolute?
	if path.IsAbs(executable) {
		return executable, nil
	}

	envPath, _ := s.Env.Get("PATH")
	fileExtensions, _ := s.Env.Get("PATHEXT") // For searching .exe, .bat, etc on Windows

	// Use our custom lookPath that takes a specific path
	absolutePath, err := LookPath(executable, envPath, fileExtensions)
	if err != nil {
		return "", err
	}

	// Since the path returned by LookPath is relative to the current working
	// directory, we need to get the absolute version of that.
	return filepath.Abs(absolutePath)
}

// Interrupt running command
func (s *Shell) Interrupt() {
	s.cmdLock.Lock()
	defer s.cmdLock.Unlock()

	if s.cmd != nil && s.cmd.proc != nil {
		s.cmd.proc.Interrupt()
	}
}

// Terminate running command
func (s *Shell) Terminate() {
	s.cmdLock.Lock()
	defer s.cmdLock.Unlock()

	if s.cmd != nil && s.cmd.proc != nil {
		s.cmd.proc.Terminate()
	}
}

// Returns the WaitStatus of the shell's process.
//
// The shell must have been started.
func (s *Shell) WaitStatus() (process.WaitStatus, error) {
	s.cmdLock.Lock()
	defer s.cmdLock.Unlock()

	if s.cmd == nil || s.cmd.proc == nil {
		return nil, errors.New("shell not started")
	}
	return s.cmd.proc.WaitStatus(), nil
}

// LockFile is a pid-based lock for cross-process locking
type LockFile interface {
	Unlock() error
}

func (s *Shell) lockfile(ctx context.Context, path string, timeout time.Duration) (LockFile, error) {
	absolutePathToLock, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("Failed to find absolute path to lock \"%s\" (%v)", path, err)
	}

	lock, err := lockfile.New(absolutePathToLock)
	if err != nil {
		return nil, fmt.Errorf("Failed to create lock \"%s\" (%s)", absolutePathToLock, err)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		// Keep trying the lock until we get it
		if err := lock.TryLock(); err != nil {
			s.Commentf("Could not acquire lock on \"%s\" (%s)", absolutePathToLock, err)
			s.Commentf("Trying again in %s...", lockRetryDuration)
			time.Sleep(lockRetryDuration)
		} else {
			break
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			// No value ready, moving on
		}
	}

	return &lock, err
}

func (s *Shell) flock(ctx context.Context, path string, timeout time.Duration) (LockFile, error) {
	absolutePathToLock, err := filepath.Abs(path + "f") // + "f" to ensure that flocks and lockfiles never share a filename
	if err != nil {
		return nil, fmt.Errorf("Failed to find absolute path to lock \"%s\" (%v)", path, err)
	}

	lock := flock.New(absolutePathToLock)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		// Keep trying the lock until we get it
		if gotLock, err := lock.TryLock(); !gotLock || err != nil {
			if err != nil {
				s.Commentf("Could not acquire lock on \"%s\" (%s)", absolutePathToLock, err)
			} else {
				s.Commentf("Could not acquire lock on \"%s\" (Locked by other process)", absolutePathToLock)
			}
			s.Commentf("Trying again in %s...", lockRetryDuration)
			time.Sleep(lockRetryDuration)
		} else {
			break
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			// No value ready, moving on
		}
	}

	return lock, err
}

// Create a cross-process file-based lock based on pid files
func (s *Shell) LockFile(ctx context.Context, path string, timeout time.Duration) (LockFile, error) {
	if experiments.IsEnabled(experiments.FlockFileLocks) {
		s.Commentf("Using flock-file-locks experiment 🧪")
		return s.flock(ctx, path, timeout)
	}
	return s.lockfile(ctx, path, timeout)
}

// Run runs a command, write stdout and stderr to the logger and return an error
// if it fails
func (s *Shell) Run(ctx context.Context, command string, arg ...string) error {
	formatted := process.FormatCommand(command, arg)
	if s.stdin == nil {
		s.Promptf("%s", formatted)
	} else {
		// bash-syntax-compatible indication that input is coming from somewhere
		s.Promptf("%s < /dev/stdin", formatted)
	}

	return s.RunWithoutPrompt(ctx, command, arg...)
}

// RunWithoutPrompt runs a command, writes stdout and err to the logger,
// and returns an error if it fails. It doesn't show a prompt.
func (s *Shell) RunWithoutPrompt(ctx context.Context, command string, arg ...string) error {
	cmd, err := s.buildCommand(command, arg...)
	if err != nil {
		s.Errorf("Error building command: %v", err)
		return err
	}

	return s.executeCommand(ctx, cmd, s.Writer, executeFlags{
		Stdout: true,
		Stderr: true,
		PTY:    s.PTY,
	})
}

// RunAndCapture runs a command and captures the output for processing. Stdout is captured, but
// stderr isn't. If the shell is in debug mode then the command will be eched and both stderr
// and stdout will be written to the logger. A PTY is never used for RunAndCapture.
func (s *Shell) RunAndCapture(ctx context.Context, command string, arg ...string) (string, error) {
	if s.Debug {
		s.Promptf("%s", process.FormatCommand(command, arg))
	}

	cmd, err := s.buildCommand(command, arg...)
	if err != nil {
		return "", err
	}

	var b bytes.Buffer

	err = s.executeCommand(ctx, cmd, &b, executeFlags{
		Stdout: true,
		Stderr: false,
		PTY:    false,
	})
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(b.String()), nil
}

// injectTraceCtx adds tracing information to the given env vars to support
// distributed tracing across jobs/builds.
func (s *Shell) injectTraceCtx(ctx context.Context, env *env.Environment) {
	span := opentracing.SpanFromContext(ctx)
	// Not all shell runs will have tracing (nor do they really need to).
	if span == nil {
		return
	}
	if err := tracetools.EncodeTraceContext(span, env.Dump()); err != nil {
		if s.Debug {
			s.Logger.Warningf("Failed to encode trace context: %v", err)
		}
		return
	}
}

// RunScript is like Run, but the target is an interpreted script which has
// some extra checks to ensure it gets to the correct interpreter. Extra environment vars
// can also be passed the script
func (s *Shell) RunScript(ctx context.Context, path string, extra *env.Environment) error {
	var command string
	var args []string

	// we apply a variety of "feature detection checks" to figure out how we should
	// best run the script

	isSh := filepath.Ext(path) == "" || filepath.Ext(path) == ".sh"
	isWindows := runtime.GOOS == "windows"
	isPwsh := filepath.Ext(path) == ".ps1"

	switch {
	case isWindows && isSh:
		if s.Debug {
			s.Commentf("Attempting to run %s with Bash for Windows", path)
		}
		// Find Bash, either part of Cygwin or MSYS. Must be in the path
		bashPath, err := s.AbsolutePath("bash.exe")
		if err != nil {
			return fmt.Errorf("Error finding bash.exe, needed to run scripts: %v. "+
				"Is Git for Windows installed and correctly in your PATH variable?", err)
		}
		command = bashPath
		args = []string{filepath.ToSlash(path)}

	case isWindows && isPwsh:
		if s.Debug {
			s.Commentf("Attempting to run %s with Powershell", path)
		}
		command = "powershell.exe"
		args = []string{"-file", path}

	case !isWindows && isSh:
		// If the script contains a shebang line, it can be run directly,
		// with the shebang line choosing the interpreter.
		sb, err := shellscript.ShebangLine(path)
		if err == nil && sb != "" {
			command = path
			args = nil
			break
		}

		// Bash was the default, so must remain the default.
		shPath, err := s.AbsolutePath("bash")
		if err != nil {
			// It's increasingly popular to not include bash in more minimal
			// container images (e.g. Alpine-based). But because bash has been
			// assumed for so long, many hooks and plugins will be written
			// assuming Bash features.
			// Emit a warning, keep calm, and carry on.
			s.Warningf("Couldn't find bash (%v). Attempting to fall back to sh. This may cause issues for hooks and plugins that assume Bash features.", err)
			shPath, err = s.AbsolutePath("sh")
			if err != nil {
				return fmt.Errorf("Error finding a shell, needed to run scripts: %v.", err)
			}
		}
		command = shPath
		args = []string{path}

	default:
		// Something else.
		command = path
		args = nil
	}

	cmd, err := s.buildCommand(command, args...)
	if err != nil {
		s.Errorf("Error building command: %v", err)
		return err
	}

	// Combine the two slices of env, let the latter overwrite the former
	environ := env.FromSlice(cmd.Env)
	environ.Merge(extra)
	cmd.Env = environ.ToSlice()

	return s.executeCommand(ctx, cmd, s.Writer, executeFlags{
		Stdout: true,
		Stderr: true,
		PTY:    s.PTY,
	})
}

type command struct {
	process.Config
	proc *process.Process
}

// buildCommand returns a command that can later be executed
func (s *Shell) buildCommand(name string, arg ...string) (*command, error) {
	// Always use absolute path as Windows has a hard time
	// finding executables in its path
	absPath, err := s.AbsolutePath(name)
	if err != nil {
		return nil, err
	}

	cfg := process.Config{
		Path:            absPath,
		Args:            arg,
		Env:             s.Env.ToSlice(),
		Stdin:           s.stdin,
		Dir:             s.wd,
		InterruptSignal: s.InterruptSignal,
	}

	// Add env that commands expect a shell to set
	cfg.Env = append(cfg.Env,
		"PWD="+s.wd,
	)

	return &command{Config: cfg}, nil
}

type executeFlags struct {
	// Whether to capture stdout
	Stdout bool

	// Whether to capture stderr
	Stderr bool

	// Run the command in a PTY
	PTY bool
}

func round(d time.Duration) time.Duration {
	// The idea here is to show 5 significant digits worth of time.
	// If your build takes 2 hours, you probably don't care about the timing
	// being reported down to the microsecond.
	switch {
	case d < 100*time.Microsecond:
		return d
	case d < time.Millisecond:
		return d.Round(10 * time.Nanosecond)
	case d < 10*time.Millisecond:
		return d.Round(100 * time.Nanosecond)
	case d < 100*time.Millisecond:
		return d.Round(time.Microsecond)
	case d < time.Second:
		return d.Round(10 * time.Microsecond)
	case d < 10*time.Second:
		return d.Round(100 * time.Microsecond)
	case d < time.Minute:
		return d.Round(time.Millisecond)
	case d < 10*time.Minute:
		return d.Round(10 * time.Millisecond)
	case d < time.Hour:
		return d.Round(100 * time.Millisecond)
	default:
		return d.Round(10 * time.Second)
	}
}

func (s *Shell) executeCommand(ctx context.Context, cmd *command, w io.Writer, flags executeFlags) error {
	// Combine the two slices of env, let the latter overwrite the former
	tracedEnv := env.FromSlice(cmd.Env)
	s.injectTraceCtx(ctx, tracedEnv)
	cmd.Env = tracedEnv.ToSlice()

	s.cmdLock.Lock()
	s.cmd = cmd
	s.cmdLock.Unlock()

	cmdStr := process.FormatCommand(cmd.Path, cmd.Args)

	if s.Debug {
		t := time.Now()
		defer func() {
			s.Commentf("↳ Command completed in %v", round(time.Since(t)))
		}()
	}

	cfg := cmd.Config

	// Modify process config based on execution flags
	if flags.PTY {
		cfg.PTY = true
		cfg.Stdout = w
	} else {
		// Show stdout if requested or via debug
		if flags.Stdout {
			cfg.Stdout = w
		} else if s.Debug {
			stdOutStreamer := NewLoggerStreamer(s.Logger)
			defer stdOutStreamer.Close()
			cfg.Stdout = stdOutStreamer
		}

		// Show stderr if requested or via debug
		if flags.Stderr {
			cfg.Stderr = w
		} else if s.Debug {
			stdErrStreamer := NewLoggerStreamer(s.Logger)
			defer stdErrStreamer.Close()
			cfg.Stderr = stdErrStreamer
		}
	}

	p := process.New(logger.Discard, cfg)

	s.cmdLock.Lock()
	s.cmd.proc = p
	s.cmdLock.Unlock()

	if err := p.Run(ctx); err != nil {
		return fmt.Errorf("Error running %q: %w", cmdStr, err)
	}

	return p.WaitResult()
}

// GetExitCode extracts an exit code from an error where the platform supports it,
// otherwise returns 0 for no error and 1 for an error
func GetExitCode(err error) int {
	if err == nil {
		return 0
	}

	if cause := new(ExitError); errors.As(err, &cause) {
		return cause.Code
	}

	if cause := new(exec.ExitError); errors.As(err, &cause) {
		return cause.ExitCode()
	}
	return 1
}

// IsExitSignaled returns true if the error is an ExitError that was
// caused by receiving a signal
func IsExitSignaled(err error) bool {
	if err == nil {
		return false
	}
	if exitErr := new(exec.ExitError); errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return status.Signaled()
		}
	}
	return false
}

func IsExitError(err error) bool {
	if cause := new(ExitError); errors.As(err, &cause) {
		return true
	}
	if cause := new(exec.ExitError); errors.As(err, &cause) {
		return true
	}
	return false
}

// ExitError is an error that carries a shell exit code
type ExitError struct {
	Code    int
	Message string
}

// Error returns the string message and fulfils the error interface
func (ee *ExitError) Error() string {
	return ee.Message
}
