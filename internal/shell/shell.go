// Package shell provides a cross-platform virtual shell abstraction for
// executing commands.
//
// It is intended for internal use by buildkite-agent only.
package shell

import (
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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/olfactor"
	"github.com/buildkite/agent/v3/internal/shellscript"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/process"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/shellwords"
	"github.com/gofrs/flock"
	"github.com/opentracing/opentracing-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const lockRetryDuration = time.Second

// ErrShellNotStarted is returned when the shell has not started a process.
var ErrShellNotStarted = errors.New("shell not started")

// Shell represents a virtual shell, handles logging, executing commands and
// provides hooks for capturing output and exit conditions.
//
// Provides a lowest-common denominator abstraction over macOS, Linux and Windows
type Shell struct {
	Logger

	// The running environment for the shell.
	Env *env.Environment

	// If set, the command arg vectors are appended to the slice as they are
	// executed (or would be executed, as in dry-run mode).
	commandLog *[][]string

	// Whether to run the shell in debug mode
	debug bool

	// Whether to actually execute commands.
	dryRun bool

	// The signal to use to interrupt the process.
	interruptSignal process.Signal

	// The currently-running or last-run process.
	proc atomic.Pointer[process.Process]

	// Whether the shell is a PTY.
	pty bool

	// Amount of time to wait between sending the InterruptSignal and SIGKILL
	signalGracePeriod time.Duration

	// stdin is an optional input stream used by Run() and friends.
	// It remains unexported on the assumption that it's not useful except via
	// CloneWithStdin to get a clone prepared for a single command that needs
	// input.
	stdin io.Reader

	// Where stdout (and usually stderr) of the process is written
	// (similar to a real shell, that displays both in the same stream).
	// Defaults to [os.Stdout].
	stdout io.Writer

	// How to encode trace contexts.
	traceContextCodec tracetools.Codec

	// Current working directory that shell commands get executed in
	wd string
}

type NewShellOpt = func(*Shell)

func WithCommandLog(log *[][]string) NewShellOpt { return func(s *Shell) { s.commandLog = log } }
func WithDebug(d bool) NewShellOpt               { return func(s *Shell) { s.debug = d } }
func WithDryRun(d bool) NewShellOpt              { return func(s *Shell) { s.dryRun = d } }
func WithEnv(e *env.Environment) NewShellOpt     { return func(s *Shell) { s.Env = e } }
func WithLogger(l Logger) NewShellOpt            { return func(s *Shell) { s.Logger = l } }
func WithPTY(pty bool) NewShellOpt               { return func(s *Shell) { s.pty = pty } }
func WithStdout(w io.Writer) NewShellOpt         { return func(s *Shell) { s.stdout = w } }
func WithWD(wd string) NewShellOpt               { return func(s *Shell) { s.wd = wd } }

func WithInterruptSignal(sig process.Signal) NewShellOpt {
	return func(s *Shell) { s.interruptSignal = sig }
}

func WithSignalGracePeriod(d time.Duration) NewShellOpt {
	return func(s *Shell) { s.signalGracePeriod = d }
}

func WithTraceContextCodec(c tracetools.Codec) NewShellOpt {
	return func(s *Shell) { s.traceContextCodec = c }
}

// New returns a new Shell. The default stdout is [os.Stdout], the default logger
// writes to [os.Stderr], the initial working directory is the result of calling
// [os.Getwd], the default environment variable set is read from [os.Environ],
// and the default trace context encoding is Gob.
func New(opts ...NewShellOpt) (*Shell, error) {
	// Start with an empty shell.
	shell := &Shell{}

	// Apply all the options to it.
	for _, opt := range opts {
		opt(shell)
	}

	// Set defaults for the important options, if not provided.
	if shell.Logger == nil {
		shell.Logger = &WriterLogger{Writer: os.Stderr, Ansi: true}
	}
	if shell.Env == nil {
		shell.Env = env.FromSlice(os.Environ())
	}
	if shell.stdout == nil {
		shell.stdout = os.Stdout
	}
	if shell.traceContextCodec == nil {
		shell.traceContextCodec = tracetools.CodecGob{}
	}
	if shell.wd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("Failed to find current working directory: %w", err)
		}
		shell.wd = wd
	}

	return shell, nil
}

// CloneWithStdin returns a copy of the Shell with the provided [io.Reader] set
// as the Stdin for the next command. The copy should be discarded after one
// command.
// For example:
//
//	sh.CloneWithStdin(strings.NewReader("hello world")).Run("cat")
func (s *Shell) CloneWithStdin(r io.Reader) *Shell {
	// Can't copy struct like `newsh := *s` because atomics can't be copied.
	return &Shell{
		Logger:            s.Logger,
		Env:               s.Env,
		stdin:             r, // our new stdin
		stdout:            s.stdout,
		wd:                s.wd,
		interruptSignal:   s.interruptSignal,
		signalGracePeriod: s.signalGracePeriod,
		traceContextCodec: s.traceContextCodec,
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

// Interrupt interrupts the running process, if there is one.
func (s *Shell) Interrupt() error { return s.proc.Load().Interrupt() }

// Terminate terminates the running process, if there is one.
func (s *Shell) Terminate() error { return s.proc.Load().Terminate() }

// Returns the WaitStatus of the shell's process.
//
// The shell must have started at least one process.
func (s *Shell) WaitStatus() (process.WaitStatus, error) {
	p := s.proc.Load()
	if p == nil {
		return nil, ErrShellNotStarted
	}
	return p.WaitStatus(), nil
}

// Unlocker implementations are things that can be unlocked, such as a
// cross-process lock. This interface exists for implementation-hiding.
type Unlocker interface {
	Unlock() error
}

// LockFile creates a cross-process file-based lock. To set a timeout on
// attempts to acquire the lock, pass a context with a timeout.
func (s *Shell) LockFile(ctx context.Context, path string) (Unlocker, error) {
	// + "f" to ensure that flocks and lockfiles (a similar lock library used by
	// old agent versions) never share a filename.
	absolutePathToLock, err := filepath.Abs(path + "f")
	if err != nil {
		return nil, fmt.Errorf("failed to find absolute path to lock %q: %w", path, err)
	}

	lock := flock.New(absolutePathToLock)

retryLoop:
	for {
		// Keep trying the lock until we get it
		gotLock, err := lock.TryLock()
		switch {
		case err != nil:
			s.Commentf("Could not acquire lock on %q (%v)", absolutePathToLock, err)
			return nil, err

		case !gotLock:
			s.Commentf("Could not acquire lock on %q (locked by another process)", absolutePathToLock)

		default:
			break retryLoop
		}

		s.Commentf("Trying again in %v...", lockRetryDuration)
		timer := time.NewTimer(lockRetryDuration)
		defer timer.Stop()

		select {
		case <-timer.C:
			// Ready to retry!

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return lock, nil
}

// Command represents a command that can be run in a shell.
type Command struct {
	shell   *Shell
	command string
	args    []string
}

// Command returns a command that can be run in the shell.
func (s *Shell) Command(command string, args ...string) Command {
	return Command{
		shell:   s,
		command: command,
		args:    args,
	}
}

// Script returns a command that runs a script in the shell. The path is either
// executed directly, or some kind of intepreter is executed in order to
// interpret it (loosely: powershell.exe for .ps1 files, bash(.exe) for shell
// scripts without shebang lines).
func (s *Shell) Script(path string) (Command, error) {
	var command string
	var args []string

	// we apply a variety of "feature detection checks" to figure out how we should
	// best run the script

	isSh := filepath.Ext(path) == "" || filepath.Ext(path) == ".sh"
	isWindows := runtime.GOOS == "windows"
	isPwsh := filepath.Ext(path) == ".ps1"

	switch {
	case isWindows && isSh:
		if s.debug {
			s.Commentf("Attempting to run %s with Bash for Windows", path)
		}
		// Find Bash, either part of Cygwin or MSYS. Must be in the path
		bashPath, err := s.AbsolutePath("bash.exe")
		if err != nil {
			return Command{}, fmt.Errorf("Error finding bash.exe, needed to run scripts: %v. "+
				"Is Git for Windows installed and correctly in your PATH variable?", err)
		}
		command = bashPath
		args = []string{filepath.ToSlash(path)}

	case isWindows && isPwsh:
		if s.debug {
			s.Commentf("Attempting to run %s with Powershell", path)
		}
		command = "powershell.exe"
		args = []string{"-file", path}

	case !isWindows && isSh:
		// If the script contains a shebang line, it can be run directly,
		// with the shebang line choosing the interpreter.
		// note that this means that it isn't necessarily a shell script in this case!
		// #!/usr/bin/env python would be totally valid, and would execute as a python script
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
				return Command{}, fmt.Errorf("error finding a shell, needed to run scripts: %w", err)
			}
		}
		command = shPath
		args = []string{path}

	default:
		// Something else.
		command = path
		args = nil
	}

	return Command{
		shell:   s,
		command: command,
		args:    args,
	}, nil
}

// Run runs the command and waits for it to complete.
func (c Command) Run(ctx context.Context, opts ...RunCommandOpt) error {
	cfg := runConfig{
		showPrompt: true,
		showStderr: true,
	}
	for _, o := range opts {
		o(&cfg)
	}

	// If prompt is enabled, or we're in debug mode, display the "prompt" showing
	// the command being run.
	if cfg.showPrompt || c.shell.debug {
		formatted := process.FormatCommand(c.command, c.args)
		if c.shell.stdin == nil {
			c.shell.Promptf("%s", formatted)
		} else {
			// bash-syntax-compatible indication that input is coming from somewhere
			c.shell.Promptf("%s < /dev/stdin", formatted)
		}
	}

	// Build the process config for the command.
	cmdCfg, err := c.shell.buildCommand(c.command, c.args...)
	if err != nil {
		c.shell.Errorf("Error building command: %v", err)
		return err
	}

	// Merge in any extra env vars.
	if cfg.extraEnv != nil {
		environ := env.FromSlice(cmdCfg.Env)
		environ.Merge(cfg.extraEnv)
		cmdCfg.Env = environ.ToSlice()
	}

	// By default, PTY and stdout are whatever the shell is configured with.
	pty := c.shell.pty
	stdout := c.shell.stdout

	// If stdout is being captured, capture it to a string builder. Also we
	// don't use a PTY.
	if cfg.captureStdout != nil {
		pty = false
		sb := new(strings.Builder)
		stdout = sb
		defer func() { *cfg.captureStdout = strings.TrimSpace(sb.String()) }()
	}

	// Redirect stderr to the shell's usual stdout, unless it is discarded.
	stderr := c.shell.stdout
	if !cfg.showStderr {
		stderr = io.Discard
	}

	// If we're performing a string search, wrap the current stdout and stderr
	// in olfactors, and report which ones were detected through the map.
	if cfg.smells != nil {
		smells := make([]string, 0, len(cfg.smells))
		for s := range cfg.smells {
			smells = append(smells, s)
		}
		so, o1 := olfactor.New(stdout, smells)
		se, o2 := olfactor.New(stderr, smells)
		stdout, stderr = so, se

		defer func() {
			for _, smelt := range o1.AllSmelt() {
				cfg.smells[smelt] = true
			}
			for _, smelt := range o2.AllSmelt() {
				cfg.smells[smelt] = true
			}
		}()
	}

	return c.shell.executeCommand(ctx, cmdCfg, stdout, stderr, pty)
}

// RunAndCaptureStdout is Run, but automatically sets options:
// * ShowPrompt(false) (overridable)
// * CaptureStdout() (not overridable)
// and returns the captured output.
func (c Command) RunAndCaptureStdout(ctx context.Context, opts ...RunCommandOpt) (string, error) {
	var capture string
	opts = append([]RunCommandOpt{ShowPrompt(false)}, opts...)
	opts = append(opts, CaptureStdout(&capture))
	err := c.Run(ctx, opts...)
	return capture, err
}

type runConfig struct {
	captureStdout *string
	showPrompt    bool
	showStderr    bool
	extraEnv      *env.Environment
	smells        map[string]bool
}

// RunCommandOpt is the type of functional options that can be passed to
// Command.Run.
type RunCommandOpt = func(*runConfig)

// CaptureStdout captures the entire stdout stream to a string instead of the
// shell's stdout. By default, it is not captured. The string pointer is
// updated with the stdout of the process after it has exited.
func CaptureStdout(s *string) RunCommandOpt { return func(c *runConfig) { c.captureStdout = s } }

// ShowStderr can be used to hide stderr from the shell's stdout. By default,
// it is enabled (the process stderr is directed to the shell's stdout).
func ShowStderr(show bool) RunCommandOpt { return func(c *runConfig) { c.showStderr = show } }

// ShowPrompt causes the command and arguments being run to be printed in the
// shell's stdout. By default this is enabled (prompt is shown).
func ShowPrompt(show bool) RunCommandOpt { return func(c *runConfig) { c.showPrompt = show } }

// WithExtraEnv can be used to set additional env vars for this run.
func WithExtraEnv(e *env.Environment) RunCommandOpt { return func(c *runConfig) { c.extraEnv = e } }

// WithStringSearch causes both the stdout and stderr streams of the process to
// be searched for strings. (This does not require capturing either stream in
// full.) After the process is finished, the map can be inspected to see which
// ones were observed.
func WithStringSearch(m map[string]bool) RunCommandOpt { return func(c *runConfig) { c.smells = m } }

// injectTraceCtx adds tracing information to the given env vars to support
// distributed tracing across jobs/builds.
func (s *Shell) injectTraceCtx(ctx context.Context, env *env.Environment) {
	// OpenTracing path (for Datadog backend)
	if span := opentracing.SpanFromContext(ctx); span != nil {
		if err := tracetools.EncodeTraceContext(span, env.Dump(), s.traceContextCodec); err != nil {
			if s.debug {
				s.Warningf("Failed to encode trace context: %v", err)
			}
		}
		return
	}

	// OpenTelemetry path (for OpenTelemetry backend)
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		carrier := propagation.MapCarrier{}
		otel.GetTextMapPropagator().Inject(ctx, carrier)

		// Transform HTTP header names to environment variable names per
		// https://opentelemetry.io/docs/specs/otel/context/env-carriers/
		// Examples: "traceparent" -> "TRACEPARENT", "X-B3-TraceId" -> "X_B3_TRACEID"
		//
		// It remains unclear whether various ecosystems are well equipped handling normalized env vars.
		// But it will be trivial to conform to the standard.
		// We shall see how community responds to this.
		for k, v := range carrier {
			envKey := strings.ToUpper(strings.ReplaceAll(k, "-", "_"))
			env.Set(envKey, v)
		}
	}
}

// buildCommand returns a command that can later be executed.
func (s *Shell) buildCommand(name string, arg ...string) (process.Config, error) {
	// Always use absolute path as Windows has a hard time
	// finding executables in its path
	absPath, err := s.AbsolutePath(name)
	if err != nil {
		return process.Config{}, err
	}

	return process.Config{
		Path:              absPath,
		Args:              arg,
		Env:               append(s.Env.ToSlice(), "PWD="+s.wd),
		Stdin:             s.stdin,
		Dir:               s.wd,
		InterruptSignal:   s.interruptSignal,
		SignalGracePeriod: s.signalGracePeriod,
	}, nil
}

// executeCommand executes a command.
//
// To ignore an output stream, you can use either nil or io.Discard:
//
//	s.executeCommand(ctx, cmd, nil, nil, pty)  // ignore both
//	s.executeCommand(ctx, cmd, writer, nil, pty) // ignore stderr
//	s.executeCommand(ctx, cmd, writer, writer, pty) // send both to same writer
//	s.executeCommand(ctx, cmd, writer1, writer2, false)
//
// Note that if pty = true, only the stdout writer will be used.
func (s *Shell) executeCommand(ctx context.Context, cmdCfg process.Config, stdout, stderr io.Writer, pty bool) error {
	// Combine the two slices of env, let the latter overwrite the former
	tracedEnv := env.FromSlice(cmdCfg.Env)
	s.injectTraceCtx(ctx, tracedEnv)
	cmdCfg.Env = tracedEnv.ToSlice()

	if s.debug {
		t := time.Now()
		defer func() {
			s.Commentf("↳ Command completed in %v", round(time.Since(t)))
		}()
	}

	cmdCfg.PTY = pty
	cmdCfg.Stdout = stdout
	cmdCfg.Stderr = stderr

	if cmdCfg.Stdout == nil {
		cmdCfg.Stdout = io.Discard
	}
	if cmdCfg.Stderr == nil {
		cmdCfg.Stderr = io.Discard
	}

	var processLogger logger.Logger
	processLogger = logger.Discard

	if s.debug {
		// Display normally-hidden output streams using log streamer.
		if cmdCfg.Stdout == io.Discard {
			stdOutStreamer := NewLoggerStreamer(s.Logger)
			defer stdOutStreamer.Close() //nolint:errcheck // If this fails, YOLO?
			cmdCfg.Stdout = stdOutStreamer
		}

		if cmdCfg.Stderr == io.Discard {
			stdErrStreamer := NewLoggerStreamer(s.Logger)
			defer stdErrStreamer.Close() //nolint:errcheck // If this fails, YOLO?
			cmdCfg.Stderr = stdErrStreamer
		}

		// This should respect the log format we set for the agent
		processLogger = logger.NewConsoleLogger(logger.NewTextPrinter(cmdCfg.Stderr), os.Exit)
	}

	if s.commandLog != nil {
		*s.commandLog = append(*s.commandLog,
			append([]string{cmdCfg.Path}, cmdCfg.Args...),
		)
	}

	if s.dryRun {
		return nil
	}

	p := process.New(processLogger, cmdCfg)
	s.proc.Store(p)

	if err := p.Run(ctx); err != nil {
		return fmt.Errorf("error running %q: %w", process.FormatCommand(cmdCfg.Path, cmdCfg.Args), err)
	}

	return p.WaitResult()
}

// ExitCode extracts an exit code from an error where the platform supports it,
// otherwise returns 0 for no error and 1 for an error
func ExitCode(err error) int {
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
// caused by receiving a signal.
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

// IsExitError reports whether err is an [ExitError] or [exec.ExitError].
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
	Code int
	Err  error
}

func (ee *ExitError) Error() string { return ee.Err.Error() }

func (ee *ExitError) Unwrap() error { return ee.Err }

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
