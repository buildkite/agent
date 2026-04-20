package job

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/roko"
)

var sshKeyscanRetryInterval = 2 * time.Second

func sshKeyScan(ctx context.Context, sh *shell.Shell, host string) (string, error) {
	toolsDir, err := findPathToSSHTools(ctx, sh)
	if err != nil {
		return "", err
	}

	sshKeyScanPath := filepath.Join(toolsDir, "ssh-keyscan")
	hostParts := strings.Split(host, ":")

	r := roko.NewRetrier(
		roko.WithMaxAttempts(3),
		roko.WithStrategy(roko.Constant(sshKeyscanRetryInterval)),
	)
	return roko.DoFunc(ctx, r, func(r *roko.Retrier) (string, error) {
		sshKeyScanCommand := fmt.Sprintf("ssh-keyscan %q", host)
		args := []string{host}

		// `ssh-keyscan` needs `-p` when scanning a host with a port
		if len(hostParts) == 2 {
			sshKeyScanCommand = fmt.Sprintf("ssh-keyscan -p %q %q", hostParts[1], hostParts[0])
			args = []string{"-p", hostParts[1], hostParts[0]}
		}

		out, err := sh.Command(sshKeyScanPath, args...).RunAndCaptureStdout(ctx)
		if err != nil {
			keyScanError := fmt.Errorf("`%s` failed", sshKeyScanCommand)
			sh.Warningf("%s (%s)", keyScanError, r)
			return "", keyScanError
		}
		if strings.TrimSpace(out) == "" {
			// Older versions of ssh-keyscan would exit 0 but not
			// return anything, and we've observed newer versions
			// of ssh-keyscan - just sometimes return no data
			// (maybe networking related?). In any case, no
			// response, means an error.
			keyScanError := fmt.Errorf("`%s` returned nothing", sshKeyScanCommand)
			sh.Warningf("%s (%s)", keyScanError, r)
			return "", keyScanError
		}

		return out, nil
	})
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
		execPath, _ := sh.Command("git", "--exec-path").RunAndCaptureStdout(ctx)
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

	return "", fmt.Errorf("unable to find ssh-keyscan: %w", err)
}
