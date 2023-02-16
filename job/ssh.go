package job

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/job/shell"
	"github.com/buildkite/roko"
)

var (
	sshKeyscanRetryInterval = 2 * time.Second
)

func sshKeyScan(ctx context.Context, sh *shell.Shell, host string) (string, error) {
	toolsDir, err := findPathToSSHTools(ctx, sh)
	if err != nil {
		return "", err
	}

	sshKeyScanPath := filepath.Join(toolsDir, "ssh-keyscan")
	hostParts := strings.Split(host, ":")
	sshKeyScanOutput := ""

	err = roko.NewRetrier(
		roko.WithMaxAttempts(3),
		roko.WithStrategy(roko.Constant(sshKeyscanRetryInterval)),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		// `ssh-keyscan` needs `-p` when scanning a host with a port
		var sshKeyScanCommand string
		if len(hostParts) == 2 {
			sshKeyScanCommand = fmt.Sprintf("ssh-keyscan -p %q %q", hostParts[1], hostParts[0])
			sshKeyScanOutput, err = sh.RunAndCapture(ctx, sshKeyScanPath, "-p", hostParts[1], hostParts[0])
		} else {
			sshKeyScanCommand = fmt.Sprintf("ssh-keyscan %q", host)
			sshKeyScanOutput, err = sh.RunAndCapture(ctx, sshKeyScanPath, host)
		}

		if err != nil {
			keyScanError := fmt.Errorf("`%s` failed", sshKeyScanCommand)
			sh.Warningf("%s (%s)", keyScanError, r)
			return keyScanError
		}
		if strings.TrimSpace(sshKeyScanOutput) == "" {
			// Older versions of ssh-keyscan would exit 0 but not
			// return anything, and we've observed newer versions
			// of ssh-keyscan - just sometimes return no data
			// (maybe networking related?). In any case, no
			// response, means an error.
			keyScanError := fmt.Errorf("`%s` returned nothing", sshKeyScanCommand)
			sh.Warningf("%s (%s)", keyScanError, r)
			return keyScanError
		}

		return nil
	})

	return sshKeyScanOutput, err
}

// On Windows, there are many horrible different versions of the ssh tools. Our
// preference is the one bundled with git for windows which is generally MinGW.
// Often this isn't in the path, so we go looking for it specifically.
//
// Some more details on the relative paths at
// https://stackoverflow.com/a/11771907
func findPathToSSHTools(ctx context.Context, sh *shell.Shell) (string, error) {
	sshKeyscan, err := sh.AbsolutePath("ssh-keyscan")
	if err == nil {
		return filepath.Dir(sshKeyscan), nil
	}

	if runtime.GOOS == "windows" {
		execPath, _ := sh.RunAndCapture(ctx, "git", "--exec-path")
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

	return "", fmt.Errorf("Unable to find ssh-keyscan: %v", err)
}
