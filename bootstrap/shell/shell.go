package shell

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/buildkite/agent/env"
	"github.com/buildkite/agent/process"
	"github.com/nightlyone/lockfile"
)

var DefaultOutput io.Writer = os.Stdout

// Shell represents a virtual shell, handles logging, executing commands and
// provides hooks for capturing output and exit conditions.
//
// Provides a lowest-common denominator abstraction over macOS, Linux and Windows
type Shell struct {
	// The running environment for the bootstrap file as each task runs
	Env *env.Environment

	// Whether the shell is a PTY
	PTY bool

	// The output of the shell
	output io.Writer

	// Current working directory that shell commands get executed in
	wd string

	// Funcs to call when the shell exits
	exitHandlers []func(err error)
}

// New returns a new Shell
func New() (*Shell, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("Failed to find current working directory: %v", err)
	}

	return &Shell{
		Env:    env.FromSlice(os.Environ()),
		wd:     wd,
		output: DefaultOutput,
	}, nil
}

// Printf prints a line of output
func (s *Shell) Printf(format string, v ...interface{}) {
	fmt.Fprintf(s.output, "%s\n", fmt.Sprintf(format, v...))
}

// Headerf prints a Buildkite formatted header
func (s *Shell) Headerf(format string, v ...interface{}) {
	fmt.Fprintf(s.output, "~~~ %s\n", fmt.Sprintf(format, v...))
}

// Commentf prints a comment line, e.g `# my comment goes here`
func (s *Shell) Commentf(format string, v ...interface{}) {
	fmt.Fprintf(s.output, "\033[90m# %s\033[0m\n", fmt.Sprintf(format, v...))
}

// Errorf shows a Buildkite formatted error expands the previous group
func (s *Shell) Errorf(format string, v ...interface{}) {
	s.Printf("\033[31m🚨 Error: %s\033[0m", fmt.Sprintf(format, v...))
	s.Printf("^^^ +++")
}

// Warningf shows a buildkite boostrap warning
func (s *Shell) Warningf(format string, v ...interface{}) {
	s.Printf("\033[33m⚠️ Warning: %s\033[0m", fmt.Sprintf(format, v...))
	s.Printf("^^^ +++")
}

// Promptf prints a shell prompt
func (s *Shell) Promptf(format string, v ...interface{}) {
	if runtime.GOOS == "windows" {
		fmt.Fprintf(s.output, "\033[90m>\033[0m %s\n", fmt.Sprintf(format, v...))
	} else {
		fmt.Fprintf(s.output, "\033[90m$\033[0m %s\n", fmt.Sprintf(format, v...))
	}
}

// Fatalf shows the error text and terminates the shell
func (s *Shell) Fatalf(format string, v ...interface{}) {
	s.Errorf(format, v...)
	s.Exit(nil)
}

func (s *Shell) AddExitHandler(f func(err error)) {
	s.exitHandlers = append(s.exitHandlers, f)
}

func (s *Shell) Exit(err error) {
	for _, f := range s.exitHandlers {
		f(err)
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

	s.Commentf("Changing working directory to \"%s\"", path)

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

	var envPath = s.Env.Get("PATH")
	var fileExtensions = s.Env.Get("PATHEXT") // For searching .exe, .bat, etc on Windows

	// Use our custom lookPath that takes a specific path
	absolutePath, err := lookPath(executable, envPath, fileExtensions)
	if err != nil {
		return "", err
	}

	// Since the path returned by LookPath is relative to the current working
	// directory, we need to get the absolute version of that.
	return filepath.Abs(absolutePath)
}

// RunCommandSilentlyAndCaptureOuput runs a command without showing a prompt or the output to the user
// and returns the output as a string
func (s *Shell) RunCommandSilentlyAndCaptureOutput(command string, args ...string) (string, error) {
	var b bytes.Buffer

	cmd, err := s.buildCommand(command, args...)
	if err != nil {
		return "", err
	}

	execErr := s.executeCommand(cmd, &b, s.PTY, true)
	output := strings.TrimSpace(b.String())

	return output, execErr
}

// RunCommand runs a command, write to the logger and return an error if it fails
func (s *Shell) RunCommand(command string, args ...string) error {
	cmd, err := s.buildCommand(command, args...)
	if err != nil {
		return err
	}

	return s.executeCommand(cmd, s.output, s.PTY, false)
}

// RunScript executes a script in a Shell, but the target is an interpreted script
// so it has extra checks applied to make sure it's executable. It also doesn't take arguments
func (s *Shell) RunScript(path string) error {
	if runtime.GOOS == "windows" {
		return s.RunCommand(path)
	}

	// If you run a script on Linux that doesn't have the
	// #!/bin/bash thingy at the top, it will fail to run with a
	// "exec format error" error. You can solve it by adding the
	// #!/bin/bash line to the top of the file, but that's
	// annoying, and people generally forget it, so we'll make it
	// easy on them and add it for them here.
	//
	// We also need to make sure the script we pass has quotes
	// around it, otherwise `/bin/bash -c run script with space.sh`
	// fails.
	return s.RunCommand("/bin/bash", "-c", fmt.Sprintf("%q", path))
}

// buildCommand returns an exec.Cmd that runs in the context of the shell
func (s *Shell) buildCommand(command string, args ...string) (*exec.Cmd, error) {
	// Always use absolute path as Windows has a hard time finding executables in it's path
	absPath, err := s.AbsolutePath(command)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(absPath, args...)
	cmd.Env = s.Env.ToSlice()
	cmd.Dir = s.wd

	return cmd, nil
}

func (s *Shell) executeCommand(cmd *exec.Cmd, w io.Writer, pty bool, silent bool) error {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGQUIT)
	defer signal.Stop(signals)

	var handleError = func(prefix string, err error) error {
		if !silent {
			s.Errorf("%s%v", prefix, err)
		}
		return err
	}

	go func() {
		// forward signals to the process
		for sig := range signals {
			if err := signalProcess(cmd, sig); err != nil {
				_ = handleError("Error passing signal to child process: ", err)
			}
		}
	}()

	cmdStr := process.FormatCommand(cmd.Path, cmd.Args)
	if !silent {
		s.Promptf("%s", cmdStr)
	}

	if pty {
		pty, err := process.StartPTY(cmd)
		if err != nil {
			return handleError("Error starting PTY: ", err)
		}

		// Copy the pty to our buffer. This will block until it EOF's
		// or something breaks.
		_, err = io.Copy(w, pty)
		if e, ok := err.(*os.PathError); ok && e.Err == syscall.EIO {
			// We can safely ignore this error, because it's just the PTY telling us
			// that it closed successfully.
			// See https://github.com/buildkite/agent/pull/34#issuecomment-46080419
		}
	} else {
		cmd.Stdout = w
		cmd.Stderr = w
		cmd.Stdin = nil

		if err := cmd.Start(); err != nil {
			return handleError(fmt.Sprintf("Error starting `%s`: ", cmdStr), err)
		}
	}

	if err := cmd.Wait(); err != nil {
		return handleError(fmt.Sprintf("Error running `%s`: ", cmdStr), err)
	}

	return nil
}

// GetExitCode extracts an exit code from an error where the platform supports it,
// otherwise returns 0 for no error and 1 for an error
func GetExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exiterr, ok := err.(*exec.ExitError); ok {
		// The program has exited with an exit code != 0
		// There is no platform independent way to retrieve
		// the exit code, but the following will work on Unix/macOS
		if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus()
		}
	}
	return 1
}

var (
	lockRetryDuration = time.Second
)

func (s *Shell) LockFile(ctx context.Context, path string) (*lockfile.Lockfile, error) {
	absolutePathToLock, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("Failed to find absolute path to lock \"%s\" (%v)", path, err)
	}

	lock, err := lockfile.New(absolutePathToLock)
	if err != nil {
		return nil, fmt.Errorf("Failed to create lock \"%s\" (%s)", absolutePathToLock, err)
	}

	for {
		// Keep trying the lock until we get it
		if err := lock.TryLock(); err != nil {
			if te, ok := err.(interface {
				Temporary() bool
			}); ok && te.Temporary() {
				s.Commentf("Could not aquire lock on \"%s\" (%s)", absolutePathToLock, err)
				s.Commentf("Trying again in %s...", lockRetryDuration)
				time.Sleep(lockRetryDuration)
			} else {
				return nil, err
			}
		} else {
			break
		}

		// Check if we've timed out
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}
	}

	return &lock, err
}

func (s *Shell) LockFileWithTimeout(path string, timeout time.Duration) (*lockfile.Lockfile, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return s.LockFile(ctx, path)
}
