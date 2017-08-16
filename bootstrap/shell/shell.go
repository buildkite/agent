package shell

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"

	"github.com/buildkite/agent/env"
)

type Shell struct {
	// The running environment for the bootstrap file as each task runs
	Env *env.Environment

	// Current working directory that shell commands get executed in
	wd string
}

func New() (*Shell, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("Failed to find current working directory: %v", err)
	}

	return &Shell{Env: env.FromSlice(os.Environ()), wd: wd}, nil
}

// CurrentWorkingDirectory returns the current working directory of the shell
func (s *Shell) CurrentWorkingDirectory() string {
	return s.wd
}

// ChangeWorkingDirectory changes the working directory of the shell
func (s *Shell) ChangeWorkingDirectory(path string) error {
	// If the path isn't absolute, prefix it with the current working directory.
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.wd, path)
	}

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

func (s *Shell) Subprocess(command string, args ...string) (*Subprocess, error) {
	// Windows has a hard time finding files that are located in folders
	// that you've added dynmically to PATH, so we'll use `AbsolutePath`
	// method (that looks for files in PATH) and use the path from that instead.
	absolutePathToCommand, err := s.AbsolutePath(command)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(absolutePathToCommand, args...)
	cmd.Env = s.Env.ToSlice()
	cmd.Dir = s.wd

	return &Subprocess{Command: cmd}, nil
}
