package utils

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
)

// NormalizeCommand has very similar semantics to `NormalizeFilePath`, except
// we only "absolute" the path if it exists on the filesystem.
func NormalizeCommand(path string) (string, error) {
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

	// if the file exists, absolute it
	if _, err := os.Stat(path); err == nil {
		// make sure its absolute
		absolutePath, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		path = absolutePath
	}

	return path, nil
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
