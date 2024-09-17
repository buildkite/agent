package osutil

import "os"

// UserHomeDir is similar to os.UserHomeDir, but prefers $HOME when available
// over other options (such as USERPROFILE on Windows).
func UserHomeDir() (string, error) {
	if h := os.Getenv("HOME"); h != "" {
		return h, nil
	}
	return os.UserHomeDir()
}
