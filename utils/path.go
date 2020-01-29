package utils

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
)

// NormalizeCommand has very similar semantics to `NormalizeFilePath`, except
// we only "absolute" the path if it exists on the filesystem. This will ensure
// that:
//
// "templates/bootstrap.sh" => "/Users/keithpitt/Development/.../templates/bootstrap.sh"
// "~/.buildkite-agent/bootstrap.sh" => "/Users/keithpitt/.buildkite-agent/bootstrap.sh"
// "cat Readme.md" => "cat Readme.md"

func NormalizeCommand(commandPath string) (string, error) {
	// don't normalize empty strings
	if commandPath == "" {
		return "", nil
	}

	// expand env and home directory
	var err error
	commandPath, err = ExpandHome(os.ExpandEnv(commandPath))
	if err != nil {
		return "", err
	}

	// if the file exists, absolute it
	if _, err := os.Stat(commandPath); err == nil {
		// make sure it's absolute
		absoluteCommandPath, err := filepath.Abs(commandPath)
		if err != nil {
			return "", err
		}
		commandPath = absoluteCommandPath
	}

	return commandPath, nil
}

// Normalizes a path and returns an clean absolute version. It correctly
// expands environment variables inside paths, converts "~/" into the users
// home directory, and replaces "./" with the current working directory.
func NormalizeFilePath(path string) (string, error) {
	// don't normalize empty strings
	if path == "" {
		return "", nil
	}

	// expand env and home directory
	var err error
	path, err = ExpandHome(os.ExpandEnv(path))
	if err != nil {
		return "", err
	}

	// make sure its absolute
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	return absolutePath, nil
}

// ExpandHome expands the path to include the home directory if the path
// is prefixed with `~`. If it isn't prefixed with `~`, the path is
// returned as-is.
// Via https://github.com/mitchellh/go-homedir/blob/master/homedir.go
func ExpandHome(path string) (string, error) {
	if len(path) == 0 {
		return path, nil
	}

	if path[0] != '~' {
		return path, nil
	}

	if len(path) > 1 && path[1] != '/' && path[1] != '\\' {
		return "", errors.New("cannot expand user-specific home dir")
	}

	usr, err := user.Current()
	if err != nil {
		return "", err
	}

	return filepath.Join(usr.HomeDir, path[1:]), nil
}
