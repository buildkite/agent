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
func NormalizeFilePath(path string) string, error {
	expandedPath := os.ExpandEnv(path)

	if len(expandedPath) > 2 {
		if expandedPath[:2] == "~/" {
			if usr, err := user.Current(); err != nil {
				return "", err
			}
			homeDir := usr.HomeDir
			return strings.Replace(expandedPath, "~", homeDir, 1), nil
		} else if expandedPath[:2] == "./" {
			if workingDir, err := os.Getwd(); err != nil {
				return "", err
			}
			return strings.Replace(expandedPath, ".", workingDir, 1), nil
		}
	}

	if absolutePath, err := filepath.Abs(expandedPath); err != nil {
		return "", err
	}

	return absolutePath
}
