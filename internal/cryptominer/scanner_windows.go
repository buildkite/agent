//go:build windows

package cryptominer

import (
	"context"
	"encoding/csv"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ScanProcessGroupPortable uses WMI via PowerShell to scan for miners in the process tree.
func ScanProcessGroupPortable(ctx context.Context, rootPID int) (ScanResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Query all processes via PowerShell/WMI
	// -NoProfile speeds up PowerShell startup
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-Command",
		"Get-CimInstance Win32_Process | Select-Object ProcessId,ParentProcessId,Name,ExecutablePath,CommandLine | ConvertTo-Csv -NoTypeInformation",
	)
	output, err := cmd.Output()
	if err != nil {
		return ScanResult{}, err
	}

	// Parse CSV output
	reader := csv.NewReader(strings.NewReader(string(output)))
	records, err := reader.ReadAll()
	if err != nil {
		return ScanResult{}, err
	}

	if len(records) < 2 {
		return ScanResult{}, nil
	}

	// Build a map of PID -> process info, and PID -> children
	type winProc struct {
		pid     int
		ppid    int
		name    string
		exePath string
		cmdline string
	}

	procByPID := make(map[int]*winProc)
	childrenOf := make(map[int][]int)

	// Skip header row (records[0])
	for _, record := range records[1:] {
		if len(record) < 5 {
			continue
		}

		pid, err := strconv.Atoi(strings.TrimSpace(record[0]))
		if err != nil {
			continue
		}

		ppid, err := strconv.Atoi(strings.TrimSpace(record[1]))
		if err != nil {
			continue
		}

		p := &winProc{
			pid:     pid,
			ppid:    ppid,
			name:    strings.TrimSpace(record[2]),
			exePath: strings.TrimSpace(record[3]),
			cmdline: strings.TrimSpace(record[4]),
		}
		procByPID[pid] = p
		childrenOf[ppid] = append(childrenOf[ppid], pid)
	}

	// Walk the process tree from rootPID to find all descendants
	var descendants []int
	queue := []int{rootPID}
	visited := make(map[int]bool)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if visited[current] {
			continue
		}
		visited[current] = true
		descendants = append(descendants, current)
		queue = append(queue, childrenOf[current]...)
	}

	// Convert to ProcessInfo and scan
	var processes []ProcessInfo
	for _, pid := range descendants {
		p, ok := procByPID[pid]
		if !ok {
			continue
		}
		processes = append(processes, ProcessInfo{
			PID:     p.pid,
			PGID:    rootPID, // Windows has no PGID; use rootPID as a stand-in
			Comm:    p.name,
			ExePath: p.exePath,
			Cmdline: p.cmdline,
		})
	}

	return scanProcesses(processes), nil
}
