package clicommand

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/buildkite/agent/v4/internal/registrationtoken"
	"github.com/urfave/cli/v3"
)

// The agent registration token is a long-lived secret, so we try to avoid
// leaving it anywhere another process on the same host could read it. If it's
// passed via the command line or environment, it's visible in
// /proc/PID/cmdline (world-readable) and /proc/PID/environ (readable by any
// process running as the same user - which jobs typically are) for the
// lifetime of the agent, because those reflect the state at exec time.
//
// To mitigate this, "agent start" supports two indirect token references in
// addition to a plain token value:
//
//   - file://PATH: read the token from the file at PATH.
//   - fd://N: read the token from (inherited) file descriptor N.
//
// On Unix, when a plaintext token is detected in the command line or
// environment, the agent re-execs itself with the token passed over a pipe as
// fd://N instead (see reexecToScrubRegistrationToken), which replaces the
// exec-time cmdline and environ with token-free versions.

const registrationTokenEnvVar = "BUILDKITE_AGENT_TOKEN"

var containerTokenRef, containerTokenValue string

var registrationStartFlags []cli.Flag

func init() {
	registrationStartFlags = append(slices.Clone(AgentStartCommand.Flags), cli.HelpFlag)
}

func ConsumeContainerToken() error {
	const key = "BUILDKITE_AGENT_CONTAINER_TOKEN_FD"
	fd, ok := os.LookupEnv(key)
	if !ok {
		return nil
	}
	if err := os.Unsetenv(key); err != nil {
		return err
	}
	ref := "fd://" + fd
	value, err := resolveRegistrationToken(ref)
	if err != nil {
		return fmt.Errorf("reading container environment token: %w", err)
	}
	containerTokenRef, containerTokenValue = ref, value
	return nil
}

// maxTokenFileSize is a sanity limit when reading tokens from files or file
// descriptors.
const maxTokenFileSize = 64 * 1024

// isIndirectToken reports whether the token value is a reference to a token
// (file:// or fd://) rather than the token itself.
func isIndirectToken(token string) bool {
	return strings.HasPrefix(token, "file://") || strings.HasPrefix(token, "fd://")
}

// resolveRegistrationToken resolves fd:// and file:// token references into
// the actual token. Plain token values are returned unchanged.
func resolveRegistrationToken(token string) (string, error) {
	if containerTokenRef != "" && token == containerTokenRef {
		return containerTokenValue, nil
	}
	switch {
	case strings.HasPrefix(token, "fd://"):
		fd, err := strconv.ParseUint(strings.TrimPrefix(token, "fd://"), 10, 31)
		if err != nil {
			return "", fmt.Errorf("invalid token file descriptor %q: %w", token, err)
		}

		f := os.NewFile(uintptr(fd), "buildkite-agent-token")
		if f == nil {
			return "", fmt.Errorf("invalid token file descriptor %q", token)
		}
		defer f.Close() //nolint:errcheck // File is only read.

		b, err := io.ReadAll(io.LimitReader(f, maxTokenFileSize))
		if err != nil {
			return "", fmt.Errorf("couldn't read token from file descriptor %d: %w", fd, err)
		}

		return strings.TrimSpace(string(b)), nil

	case strings.HasPrefix(token, "file://"):
		path := strings.TrimPrefix(token, "file://")

		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("couldn't read token from file: %w", err)
		}

		return strings.TrimSpace(string(b)), nil

	default:
		return token, nil
	}
}

// registrationTokenFromArgs scans command line arguments for the registration
// token flag (-token/--token, space- or =-separated) and returns its value.
// If the flag appears multiple times, the last value wins, matching flag
// parsing behaviour.
func registrationTokenFromArgs(args []string) (token string, found bool) {
	if len(args) == 0 {
		return "", false
	}
	return registrationtoken.ParseArgs(args[1:], registrationStartFlags).Token()
}

// replaceTokenInArgs returns a copy of args with the value of every
// registration token flag replaced with the given replacement.
func replaceTokenInArgs(args []string, replacement string) []string {
	if len(args) == 0 {
		return slices.Clone(args)
	}
	parsed := registrationtoken.ParseArgs(args[1:], registrationStartFlags)
	return append([]string{args[0]}, parsed.Replace(args[1:], replacement)...)
}

// scrubTokenFromEnviron returns a copy of environ (in "KEY=value" form) with
// any BUILDKITE_AGENT_TOKEN entries removed.
func scrubTokenFromEnviron(environ []string) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		if strings.HasPrefix(kv, registrationTokenEnvVar+"=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
