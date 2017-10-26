package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/buildkite/agent/bootstrap/shell"
)

func sshKeyScan(sh *shell.Shell, host string) (string, error) {
	toolsDir, err := findPathToSSHTools(sh)
	if err != nil {
		return "", err
	}

	parts := strings.Split(host, ":")
	if len(parts) == 2 {
		return sh.RunAndCapture(filepath.Join(toolsDir, "ssh-keyscan"), "-p", parts[1], parts[0])
	}

	out, err := sh.RunAndCapture(filepath.Join(toolsDir, "ssh-keyscan"), host)
	if err != nil {
		return out, err
	}

	// older versions of ssh-keyscan would exit 0 but not return anything
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("No keys returned for %q", host)
	}

	return out, err
}

// On Windows, there are many horrible different versions of the ssh tools. Our preference is the one bundled with
// git for windows which is generally MinGW. Often this isn't in the path, so we go looking for it specifically.
//
// Some more details on the relative paths at https://stackoverflow.com/a/11771907
func findPathToSSHTools(sh *shell.Shell) (string, error) {
	if runtime.GOOS == "windows" {
		execPath, _ := sh.RunAndCapture("git", "--exec-path")
		if len(execPath) > 0 {
			for _, path := range []string{
				filepath.Join(execPath, "..", "..", "..", "usr", "bin", "ssh-keygen.exe"),
				filepath.Join(execPath, "..", "..", "bin", "ssh-keygen.exe"),
			} {
				if _, err := os.Stat(path); err == nil {
					return filepath.Dir(path), nil
				}
			}
		}
	}
	sshKeyscan, err := sh.AbsolutePath("ssh-keyscan")
	if err != nil {
		return "", fmt.Errorf("Unable to find ssh-keyscan: %v", err)
	}
	return filepath.Dir(sshKeyscan), nil
}
