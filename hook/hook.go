package hook

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/buildkite/agent/v3/bootstrap/shell"
	"github.com/buildkite/agent/v3/utils"
)

// Find returns the absolute path to the best matching hook file in a path, or
// os.ErrNotExist if none is found
func Find(hookDir string, name string) (string, error) {
	if runtime.GOOS == "windows" {
		// check for windows types first
		if p, err := shell.LookPath(name, hookDir, ".BAT;.CMD;.PS1"); err == nil {
			return p, nil
		}
	}
	// otherwise chech for th default shell script
	if p := filepath.Join(hookDir, name); utils.FileExists(p) {
		return p, nil
	}
	// don't wrap os.ErrNotExist without checking callers handle it.
	// e.g. os.IfNotExist(err) does not handle wrapped errors.
	return "", os.ErrNotExist
}
