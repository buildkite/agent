package agent

import (
	"github.com/buildkite/agent/process"
	"runtime"
)

// Returns a dump of the raw operating system information
func OSDump() (string, error) {
	if runtime.GOOS == "darwin" {
		return process.Run("sw_vers")
	} else if runtime.GOOS == "linux" {
		return process.Cat("/etc/*-release"), nil
	} else if runtime.GOOS == "windows" {
		return process.Run("ver")
	}

	return "", nil
}
