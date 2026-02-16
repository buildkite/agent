//go:build linux

package cryptominer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ScanProcessGroup scans all processes in a given process group and returns
// information about each one. This only works on Linux.
func ScanProcessGroup(pgid int) ([]ProcessInfo, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc: %w", err)
	}

	var processes []ProcessInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue // not a PID directory
		}

		pgrp, err := readPGID(pid)
		if err != nil {
			continue // process may have exited
		}

		if pgrp != pgid {
			// It's not in our process group ie was started by the buildkite agent or one of its children, so it's none
			// of our business
			continue
		}

		processes = append(processes, getProcessDetails(pid, pgid))
	}

	return processes, nil
}

// readPGID reads only the process group ID from /proc/<pid>/stat.
func readPGID(pid int) (int, error) {
	statData, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}

	// Find the closing paren of comm (handles cases where comm contains spaces/parens)
	closeParenIdx := bytes.LastIndexByte(statData, ')')
	if closeParenIdx == -1 || closeParenIdx >= len(statData)-1 {
		return 0, fmt.Errorf("malformed stat file")
	}

	// Fields after (comm) are space-separated: state ppid pgrp ...
	fields := strings.Fields(string(statData[closeParenIdx+2:]))
	if len(fields) < 3 {
		return 0, fmt.Errorf("not enough fields in stat")
	}

	return strconv.Atoi(fields[2])
}

// getProcessDetails reads comm, cmdline, and exe for a process already known
// to be in the target process group.
func getProcessDetails(pid int, pgid int) ProcessInfo {
	info := ProcessInfo{PID: pid, PGID: pgid}

	commData, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err == nil {
		info.Comm = strings.TrimSpace(string(commData))
	}

	cmdlineData, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err == nil {
		info.Cmdline = strings.TrimSpace(strings.ReplaceAll(string(cmdlineData), "\x00", " "))
	}

	resolved, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err == nil {
		info.ExePath = resolved
	}

	return info
}

// ScanForMiners scans all processes in a process group and checks for cryptominer patterns.
func ScanForMiners(pgid int) (ScanResult, error) {
	processes, err := ScanProcessGroup(pgid)
	if err != nil {
		return ScanResult{}, err
	}

	return scanProcesses(processes), nil
}

// ScanProcessGroupPortable uses /proc to scan for miners in a process group.
func ScanProcessGroupPortable(ctx context.Context, pgid int) (ScanResult, error) {
	processes, err := ScanProcessGroup(pgid)
	if err != nil {
		return ScanResult{}, err
	}

	return scanProcesses(processes), nil
}
