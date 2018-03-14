package utils

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

// Normalizes a path and returns an clean absolute version. It correctly
// expands environment variables inside paths, converts "~/" into the users
// home directory, and replaces "./" with the current working directory.
func NormalizeFilePath(path string) (string, error) {
	expandedPath := os.ExpandEnv(path)

	if len(expandedPath) > 2 {
		if expandedPath[:2] == "~/" {
			usr, err := user.Current()
			if err != nil {
				return "", err
			}

			return strings.Replace(expandedPath, "~", usr.HomeDir, 1), nil
		} else if expandedPath[:2] == "./" {
			workingDir, err := os.Getwd()
			if err != nil {
				return "", err
			}

			return strings.Replace(expandedPath, ".", workingDir, 1), nil
		}
	}

	absolutePath, err := filepath.Abs(expandedPath)
	if err != nil {
		return "", err
	}

	return absolutePath, nil
}
