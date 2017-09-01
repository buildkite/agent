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
	"strings"
	"syscall"

	"github.com/buildkite/agent/env"
	"github.com/buildkite/agent/process"
	shellwords "github.com/mattn/go-shellwords"
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

	// Current working directory that shell commands get executed in
	wd string

	// The context for the shell
	ctx context.Context
}

// New returns a new Shell
func New() (*Shell, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("Failed to find current working directory: %v", err)
	}

	return &Shell{
		Logger: StderrLogger,
		Env:    env.FromSlice(os.Environ()),
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

// Run runs a command, write to the logger and return an error if it fails
func (s *Shell) Run(command string, args ...interface{}) error {
	words, err := shellwords.Parse(fmt.Sprintf(command, args...))
	if err != nil {
		return err
	}

	cmd, err := s.buildCommand(words[0], words[1:]...)
	if err != nil {
		s.Errorf("Error building command: %v", err)
		return err
	}

	return s.executeCommand(cmd, os.Stderr, false)
}

// RunAndCapture runs a command and captures the output, nothing else is logged
func (s *Shell) RunAndCapture(command string, args ...interface{}) (string, error) {
	words, err := shellwords.Parse(fmt.Sprintf(command, args...))
	if err != nil {
		return "", err
	}

	cmd, err := s.buildCommand(words[0], words[1:]...)
	if err != nil {
		return "", err
	}

	var b bytes.Buffer

	err = s.executeCommand(cmd, &b, true)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(b.String()), nil
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

func (s *Shell) executeCommand(cmd *exec.Cmd, w io.Writer, silent bool) error {
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

	cmdStr := process.FormatCommand(cmd.Path, cmd.Args[1:])
	if !silent {
		s.Promptf("%s", cmdStr)
	}

	if s.PTY {
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

// Quote takes a line of commands and uses shell word splitting on them and then
// quotes anything with spaces or special characters. This is used or making things
// like sets of flags safe to literally include in commands
func QuoteArguments(line string) (string, error) {
	words, err := shellwords.Parse(line)
	if err != nil {
		return "", err
	}
	for idx, word := range words {
		words[idx] = fmt.Sprintf("%q", word)
	}
	return strings.Join(words, " "), nil
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
