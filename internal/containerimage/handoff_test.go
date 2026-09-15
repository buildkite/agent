//go:build linux || darwin

package containerimage

import (
	"fmt"
	"os"
	"strings"
	"syscall"
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
		if name := os.Getenv("TOKEN_CASE"); name == "env-fd" || name == "generated-file" {
			return nil
		}
		return fmt.Errorf("missing environment handoff marker")
	}
	path := "/proc/1/fd/" + fd
	info, err := os.Stat(path)
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		return fmt.Errorf("environment handoff is not a pipe: %v", err)
	}
	handle, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer func() { _ = syscall.Close(handle) }()
	b = make([]byte, 65536)
	n, err := syscall.Read(handle, b)
	if n != 0 || err != nil {
		return fmt.Errorf("environment handoff not drained and writer closed: bytes=%d, err=%v", n, err)
	}
	return nil
}
