//go:build !windows

package system

import (
	"log/slog"
	"runtime"

	"github.com/buildkite/agent/v4/internal/process"
)

// Returns a dump of the raw operating system information
func VersionDump(l *slog.Logger) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return process.Run(l, "sw_vers")
	case "linux":
		return process.Cat("/etc/*-release")
	case "freebsd", "openbsd", "netbsd", "dragonfly", "solaris":
		return process.Run(l, "uname", "-sr")
	case "aix":
		return process.Run(l, "oslevel", "-s")
	default:
		return "", nil
	}
}
