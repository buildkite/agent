package clicommand

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

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
	return parseRegistrationTokenArgs(args[1:]).token()
}

// replaceTokenInArgs returns a copy of args with the value of every
// registration token flag replaced with the given replacement.
func replaceTokenInArgs(args []string, replacement string) []string {
	if len(args) == 0 {
		return slices.Clone(args)
	}
	return append([]string{args[0]}, parseRegistrationTokenArgs(args[1:]).replace(args[1:], replacement)...)
}

var registrationStartFlags []cli.Flag

func init() {
	registrationStartFlags = append(slices.Clone(AgentStartCommand.Flags), cli.HelpFlag)
}

type registrationArgumentFlag struct {
	cli.Flag
	takesValue bool
	record     func(string, string, bool)
}

func (f *registrationArgumentFlag) IsBoolFlag() bool { return !f.takesValue }

func (f *registrationArgumentFlag) Set(name, value string) error {
	if err := f.Flag.Set(name, value); err != nil {
		return err
	}
	f.record(name, value, f.takesValue)
	return nil
}

type registrationTokenOption struct {
	index  int
	prefix string
	value  string
}

type registrationTokenArgs struct {
	start   bool
	help    bool
	options []registrationTokenOption
}

func (a registrationTokenArgs) token() (string, bool) {
	if len(a.options) == 0 {
		return "", false
	}
	return a.options[len(a.options)-1].value, true
}

func (a registrationTokenArgs) replace(args []string, replacement string) []string {
	out := slices.Clone(args)
	for _, option := range a.options {
		out[option.index] = option.prefix + replacement
	}
	return out
}

func parseRegistrationTokenArgs(args []string) registrationTokenArgs {
	start := -1
	root := &cli.Command{
		Name: "buildkite-agent", Version: "internal", Writer: io.Discard, ErrWriter: io.Discard,
		Action: func(context.Context, *cli.Command) error { return nil },
		Commands: []*cli.Command{{
			Name: "start", SkipFlagParsing: true,
			Action: func(_ context.Context, c *cli.Command) error {
				start = len(args) - c.Args().Len() - 1
				return nil
			},
		}},
	}
	if err := root.Run(context.Background(), append([]string{"buildkite-agent"}, args...)); err != nil || start < 0 {
		return registrationTokenArgs{}
	}
	parsed := registrationTokenArgs{}
	cursor := start + 1
	record := func(name, value string, takesValue bool) {
		for cursor < len(args) {
			index := cursor
			cursor++
			arg := strings.TrimSpace(args[index])
			flagName, _, equal := strings.Cut(strings.TrimLeft(arg, "-"), "=")
			if !strings.HasPrefix(arg, "-") || flagName != name {
				continue
			}
			prefix := ""
			if takesValue {
				if equal {
					prefix = args[index][:strings.Index(args[index], "=")+1]
				} else {
					index = cursor
					cursor++
				}
			}
			if name == "token" {
				parsed.options = append(parsed.options, registrationTokenOption{index: index, prefix: prefix, value: value})
			}
			return
		}
	}
	command := &cli.Command{
		Name: "start", HideVersion: true, Writer: io.Discard, ErrWriter: io.Discard,
		Action: func(context.Context, *cli.Command) error {
			parsed.start = true
			return nil
		},
	}
	for _, definition := range registrationStartFlags {
		names := definition.Names()
		takesValue := definition.(interface{ TakesValue() bool }).TakesValue()
		var flag cli.Flag = &cli.StringFlag{Name: names[0], Aliases: names[1:]}
		if !takesValue {
			flag = &cli.BoolFlag{Name: names[0], Aliases: names[1:]}
		}
		command.Flags = append(command.Flags, &registrationArgumentFlag{Flag: flag, takesValue: takesValue, record: record})
	}
	_ = command.Run(context.Background(), args[start:])
	parsed.help = command.Bool("help")
	parsed.start = parsed.start || parsed.help
	return parsed
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
