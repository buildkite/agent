//go:build linux || darwin

package containerimage

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"syscall"

	"github.com/buildkite/agent/v4/clicommand"
	"github.com/buildkite/agent/v4/internal/registrationtoken"
	"github.com/urfave/cli/v3"
)

func inspectHandoff() error {
	b, err := os.ReadFile("/proc/1/environ")
	if err != nil {
		return err
	}
	var fd string
	for _, entry := range strings.Split(string(b), "\x00") {
		if value, ok := strings.CutPrefix(entry, "BUILDKITE_AGENT_CONTAINER_TOKEN_FD="); ok {
			fd = value
		}
	}
	if fd == "" {
		if name := os.Getenv("TOKEN_CASE"); name != "env-fd" && name != "generated-file" {
			return fmt.Errorf("missing environment handoff marker")
		}
	} else if err := inspectPipe(fd); err != nil {
		return err
	}
	b, err = os.ReadFile("/proc/1/cmdline")
	if err != nil {
		return err
	}
	args := strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
	index := slices.Index(args, "/usr/local/bin/buildkite-agent")
	if index < 0 {
		return fmt.Errorf("missing agent in tini argv")
	}
	flags := append(slices.Clone(clicommand.AgentStartCommand.Flags), cli.HelpFlag)
	parsed := registrationtoken.ParseArgs(args[index+1:], flags)
	if token, ok := parsed.Token(); ok && strings.HasPrefix(token, "fd://") {
		return inspectPipe(strings.TrimPrefix(token, "fd://"))
	}
	return nil
}

func inspectPipe(fd string) error {
	path := "/proc/1/fd/" + fd
	info, err := os.Stat(path)
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		return fmt.Errorf("token handoff %s is not a pipe: %v", fd, err)
	}
	handle, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer func() { _ = syscall.Close(handle) }()
	b := make([]byte, 65536)
	n, err := syscall.Read(handle, b)
	if n != 0 || err != nil {
		return fmt.Errorf("token handoff %s not drained and writer closed: bytes=%d, err=%v", fd, n, err)
	}
	return nil
}
