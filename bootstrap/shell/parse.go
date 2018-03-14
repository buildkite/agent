package shell

import (
	"path/filepath"

	shellwords "github.com/mattn/go-shellwords"
)

// Parse parses a shell expression into tokens
func Parse(s string) ([]string, error) {
	// Shellwords needs to be converted to slashes for windows
	args, err := shellwords.Parse(filepath.ToSlash(s))
	if err != nil {
		return nil, err
	}

	return args, nil
}
