// Package hook provides management and execution of hook scripts, and the
// ability to capture environment variable changes caused by scripts.
//
// It is intended for internal use by buildkite-agent only.
package hook

import (
	"os"
	"path/filepath"
	"runtime"
)

// Find returns the absolute path to the best matching hook file in a hookDir
// (within the given root, if one is provided), or os.ErrNotExist if none is
// found.
func Find(root *os.Root, hookDir, name string) (string, error) {
	// Figure out how to stat; if we have a root, then use that.
	stat := os.Stat
	if root != nil {
		stat = root.Stat
	}

	// exts is a list of file extensions to check.
	var exts []string
	if runtime.GOOS == "windows" {
		// check for windows types first
		exts = []string{".bat", ".cmd", ".ps1", ".exe"}
	}
	// always check for an extensionless file
	exts = append(exts, "")

	// Check for a file named name+ext in hookDir.
	for _, ext := range exts {
		p := filepath.Join(hookDir, name+ext)
		if fi, err := stat(p); err != nil || fi.IsDir() {
			continue
		}
		// If we're on Windows, there are no executable perms bits to check.
		// If we're not on Windows, then no matter what bits are set, we do
		// further inspection later on to figure out how best to run it.

		// Prepend the root's path to the relative hook dir path, if we have
		// one.
		if root != nil {
			return filepath.Join(root.Name(), p), nil
		}
		return p, nil
	}

	// Don't wrap os.ErrNotExist without checking callers handle it.
	// For example, os.IfNotExist(err) does not handle wrapped errors.
	return "", os.ErrNotExist
}
