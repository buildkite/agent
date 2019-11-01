// +build !windows

package system

import (
	"runtime"

	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/process"
)

// Returns a dump of the raw operating system information
func VersionDump(l logger.Logger) (string, error) {
	if runtime.GOOS == "darwin" {
		return process.Run(l, "sw_vers")
	} else if runtime.GOOS == "linux" {
		return process.Cat("/etc/*-release")
	}

	return "", nil
}
