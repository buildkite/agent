//go:build !linux && !darwin && !windows

package cryptominer

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// ScanProcessGroupPortable uses ps command to scan for miners.
// This is the portable fallback for Unix systems without a more specific implementation.
func ScanProcessGroupPortable(ctx context.Context, pgid int) (ScanResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ps", "-eo", "pgid,pid,command")
	output, err := cmd.Output()
	if err != nil {
		return ScanResult{}, err
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) > 1 {
		lines = lines[1:]
	}

	return ScanProcCmdline(lines, pgid)
}
