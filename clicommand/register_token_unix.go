//go:build unix

package clicommand

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// reexecToScrubRegistrationToken checks whether a plaintext registration
// token was passed via the command line or environment, and if so, re-execs
// the agent with the token passed over a pipe (as --token fd://N) instead.
//
// This exists because /proc/PID/cmdline and /proc/PID/environ expose the
// *exec-time* command line and environment for the life of the process:
// unsetting the variable after startup doesn't remove it from /proc, and jobs
// typically run as the same user as the agent, so they can read both. Since
// exec (as opposed to fork+exec) replaces the process image in place, the
// re-exec'd agent keeps the same PID with a token-free cmdline and environ.
//
// On success this function never returns. It returns nil when no re-exec is
// needed, and an error if the re-exec was attempted but failed (in which case
// the caller may continue running with the token exposed).
func reexecToScrubRegistrationToken() error {
	argToken, argFound := registrationTokenFromArgs(os.Args)
	envToken := os.Getenv(registrationTokenEnvVar)

	argPlain := argFound && argToken != "" && !isIndirectToken(argToken)
	envPlain := envToken != "" && !isIndirectToken(envToken)

	if !argPlain && !envPlain {
		// Nothing secret in the command line or environment.
		return nil
	}

	// The effective token follows flag-beats-env precedence. If the flag is
	// present but indirect (file:// or fd://), it wins and there's no secret
	// to pipe: we only need to scrub the unused env var.
	var secret string
	switch {
	case argPlain:
		secret = argToken
	case !argFound:
		secret = envToken
	}

	args := os.Args
	var tokenPipe *os.File

	if secret != "" {
		r, w, err := os.Pipe()
		if err != nil {
			return fmt.Errorf("creating token pipe: %w", err)
		}
		tokenPipe = r

		// The token is far smaller than the pipe buffer, so this write
		// completes without a reader.
		if _, err := w.WriteString(secret); err != nil {
			return fmt.Errorf("writing token to pipe: %w", err)
		}
		if err := w.Close(); err != nil {
			return fmt.Errorf("closing token pipe: %w", err)
		}

		// os.Pipe creates both ends with CLOEXEC set. The read end must
		// survive the exec; the write end should not (and its buffered
		// contents remain readable regardless).
		if _, err := unix.FcntlInt(r.Fd(), unix.F_SETFD, 0); err != nil {
			return fmt.Errorf("clearing CLOEXEC on token pipe: %w", err)
		}

		ref := fmt.Sprintf("fd://%d", r.Fd())
		if argFound {
			args = replaceTokenInArgs(args, ref)
		} else {
			args = append(append([]string{}, args...), "--token", ref)
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable: %w", err)
	}

	err = syscall.Exec(exe, args, scrubTokenFromEnviron(os.Environ()))
	if err != nil {
		// This block will always execute, because syscall.Exec doesn't return if there's no error
		tokenPipe.Close() //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("re-exec %q: %w", exe, err)
	}

	return nil // unreachable
}
