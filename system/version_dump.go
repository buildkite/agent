// +build !windows

package system

import (
	"runtime"

	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/process"
)

// Returns a dump of the raw operating system information
func VersionDump(l logger.Logger) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return process.Run(l, "sw_vers")
	case "linux":
		return process.Cat("/etc/*-release")
	case "freebsd", "openbsd", "netbsd", "dragonfly":
		return process.Run(l, "uname", "-sr")
	default:
		return "", nil
	}
}
