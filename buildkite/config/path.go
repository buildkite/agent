package config

import (
	"os"
	"os/user"
	"strings"
)

// Replaces ~/ with the users home directory, ./ with the woring directory, and
// replaces any parses any environment variables.
func NormalizeFilePath(path string) string {
	expandedPath := os.ExpandEnv(path)

	if len(expandedPath) > 2 {
		if expandedPath[:2] == "~/" {
			usr, _ := user.Current()
			dir := usr.HomeDir
			return strings.Replace(expandedPath, "~", dir, 1)
		} else if expandedPath[:2] == "./" {
			dir, _ := os.Getwd()
			return strings.Replace(expandedPath, ".", dir, 1)
		}
	}

	return expandedPath
}
