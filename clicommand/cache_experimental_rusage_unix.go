//go:build unix

package clicommand

import (
	"encoding/json"
	"os"
	"syscall"

	"github.com/buildkite/agent/v4/logger"
)

// writeExperimentalRusage writes the process's and its waited-for children's
// (such as nsc) CPU time and peak resident memory to path as JSON
// (experimental, A-1952). max_rss is in KiB on Linux.
func writeExperimentalRusage(l logger.Logger, path string) {
	usage := func(who int) map[string]int64 {
		var ru syscall.Rusage
		if err := syscall.Getrusage(who, &ru); err != nil {
			return nil
		}
		return map[string]int64{
			"user_ms": ru.Utime.Nano() / 1e6,
			"sys_ms":  ru.Stime.Nano() / 1e6,
			"max_rss": int64(ru.Maxrss),
		}
	}
	data, err := json.Marshal(map[string]any{
		"self":     usage(syscall.RUSAGE_SELF),
		"children": usage(syscall.RUSAGE_CHILDREN),
	})
	if err == nil {
		err = os.WriteFile(path, data, 0o600)
	}
	if err != nil {
		l.Warnf("Failed to write experimental rusage file: %v", err)
	}
}
