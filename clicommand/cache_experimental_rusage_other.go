//go:build !unix

package clicommand

import "github.com/buildkite/agent/v4/logger"

// writeExperimentalRusage is only implemented on Unix (experimental, A-1952).
func writeExperimentalRusage(l logger.Logger, _ string) {
	l.Warnf("Experimental rusage file is not supported on this platform")
}
