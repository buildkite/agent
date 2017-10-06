package bootstrap

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/buildkite/agent/bootstrap/shell"
	"github.com/pkg/errors"
)

func sshKeygen(sh *shell.Shell, knownHostsfile, host string) (string, error) {
	sshKeygen, err := sh.AbsolutePath("ssh-keygen")
	if err != nil && runtime.GOOS == "windows" {
		var winErr error
		if sshKeygen, winErr = findPathToSSHToolsOnWindows(sh); winErr != nil {
			return "", err
		}
	}

	return sh.RunAndCapture(sshKeygen, "-f", knownHostsfile, "-F", host)
}

func sshKeyScan(sh *shell.Shell, host string) (string, error) {
	sshKeyscan, err := sh.AbsolutePath("ssh-keyscan")
	if err != nil && runtime.GOOS == "windows" {
		var winErr error
		if sshKeyscan, winErr = findPathToSSHToolsOnWindows(sh); winErr != nil {
			return "", err
		}
	}

	return sh.RunAndCapture(sshKeyscan, host)
}

// On Windows, sometimes ssh-keygen isn't on the $PATH by default, but it's bundled with
// Git for Windows on most systems and we can find it relative to there.
//
// Some more details on the relative paths at https://stackoverflow.com/a/11771907
func findPathToSSHToolsOnWindows(sh *shell.Shell) (string, error) {
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

	return "", errors.New("Failed to find bundled ssh tools")
}
