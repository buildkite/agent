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

	"github.com/buildkite/agent/env"
	"github.com/buildkite/agent/process"
	"github.com/pkg/errors"
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

	// Where stdout is written, defaults to os.Stdout
	Writer io.Writer

	// Whether to run the shell in debug mode
	Debug bool

	// Current working directory that shell commands get executed in
	wd string

	// The context for the shell
	ctx context.Context
}

// New returns a new Shell
func New() (*Shell, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to find current working directory")
	}

	return &Shell{
		Logger: StderrLogger,
		Env:    env.FromSlice(os.Environ()),
		Writer: os.Stdout,
		wd:     wd,
		ctx:    context.Background(),
	}, nil
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

	envPath, _ := s.Env.Get("PATH")
	fileExtensions, _ := s.Env.Get("PATHEXT") // For searching .exe, .bat, etc on Windows

	// Use our custom lookPath that takes a specific path
	absolutePath, err := lookPath(executable, envPath, fileExtensions)
	if err != nil {
		return "", err
	}

	// Since the path returned by LookPath is relative to the current working
	// directory, we need to get the absolute version of that.
	return filepath.Abs(absolutePath)
}

// Run runs a command, write to the logger and return an error if it fails
func (s *Shell) Run(command string, arg ...string) error {
	s.Promptf("%s", process.FormatCommand(command, arg))

	cmd, err := s.buildCommand(command, arg...)
	if err != nil {
		s.Errorf("Error building command: %v", err)
		return err
	}

	return s.executeCommand(cmd, s.Writer, executeFlags{
		Silent: false,
		PTY:    s.PTY,
	})
}

// RunAndCapture runs a command and captures the stdout, nothing else is logged. A PTY is not used
// even if one is enabled for the shell. Will write the command and the output to logger if Debug is enabled
func (s *Shell) RunAndCapture(command string, arg ...string) (string, error) {
	if s.Debug {
		s.Promptf("%s", process.FormatCommand(command, arg))
	}

	cmd, err := s.buildCommand(command, arg...)
	if err != nil {
		return "", err
	}

	var b bytes.Buffer

	err = s.executeCommand(cmd, &b, executeFlags{
		Silent: !s.Debug,
		PTY:    false,
	})
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(b.String()), nil
}

// RunScript is like Run, but the target is an interpreted script which has
// some extra checks to ensure it gets to the correct interpreter. It also supports
// passing in extra environment just for that script
func (s *Shell) RunScript(path string, extra *env.Environment) error {
	// If you run a script on Linux that doesn't have the
	// #!/bin/bash shebang at the top, it will fail to run with a
	// "exec format error" error.

	// You can solve it by adding the
	// #!/bin/bash line to the top of the file, but that's
	// annoying, and people generally forget it, so we'll make it
	// easy on them and add it for them here.

	// We also need to make sure the script we pass has quotes
	// around it, otherwise `/bin/bash -c run script with space.sh` fails.

	var command string
	var args []string

	if runtime.GOOS == "windows" {
		command = path
		args = []string{}
	} else {
		command = "/bin/bash"
		args = []string{"-c", path}
	}

	s.Promptf("%s", process.FormatCommand(command, args))

	cmd, err := s.buildCommand(command, args...)
	if err != nil {
		s.Errorf("Error building command: %v", err)
		return err
	}

	// Combine the two slices of env, let the latter overwrite the former
	currentEnv := env.FromSlice(cmd.Env)
	customEnv := currentEnv.Merge(extra)
	cmd.Env = customEnv.ToSlice()

	return s.executeCommand(cmd, s.Writer, executeFlags{
		Silent: false,
		PTY:    s.PTY,
	})
}

// buildCommand returns an exec.Cmd that runs in the context of the shell
func (s *Shell) buildCommand(name string, arg ...string) (*exec.Cmd, error) {
	// Always use absolute path as Windows has a hard time finding executables in it's path
	absPath, err := s.AbsolutePath(name)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(absPath, arg...)
	cmd.Env = s.Env.ToSlice()
	cmd.Dir = s.wd

	return cmd, nil
}

type executeFlags struct {
	// Don't log output of commands to the shell output stream
	Silent bool

	// Run the command in a PTY
	PTY bool
}

func (s *Shell) executeCommand(cmd *exec.Cmd, w io.Writer, flags executeFlags) error {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGQUIT)
	defer signal.Stop(signals)

	go func() {
		// forward signals to the process
		for sig := range signals {
			if err := signalProcess(cmd, sig); err != nil {
				if !flags.Silent {
					s.Errorf("Error passing signal to child process: %v", err)
				}
			}
		}
	}()

	cmdStr := process.FormatCommand(cmd.Path, cmd.Args[1:])

	if flags.PTY {
		pty, err := process.StartPTY(cmd)
		if err != nil {
			return fmt.Errorf("Error starting PTY: %v", err)
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
		cmd.Stderr = nil
		cmd.Stdin = nil

		if s.Debug {
			stdOutStreamer := NewLoggerStreamer(s.Logger)
			defer stdOutStreamer.Close()

			stdErrStreamer := NewLoggerStreamer(s.Logger)
			defer stdErrStreamer.Close()

			// write the stdout to the writer and stream both stdout and stderr to the logger
			cmd.Stdout = io.MultiWriter(stdOutStreamer, w)
			cmd.Stderr = stdErrStreamer
		}

		if err := cmd.Start(); err != nil {
			return errors.Wrapf(err, "Error starting `%s`", cmdStr)
		}
	}

	if err := cmd.Wait(); err != nil {
		if s.Debug {
			s.Printf("Exited with error: %v", err)
		}

		return errors.Wrapf(err, "Error running `%s`", cmdStr)
	}

	return nil
}

// GetExitCode extracts an exit code from an error where the platform supports it,
// otherwise returns 0 for no error and 1 for an error
func GetExitCode(err error) int {
	if err == nil {
		return 0
	}
	switch cause := errors.Cause(err).(type) {
	case *exec.ExitError:
		// The program has exited with an exit code != 0
		// There is no platform independent way to retrieve
		// the exit code, but the following will work on Unix/macOS
		if status, ok := cause.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus()
		}
	}
	return 1
}
