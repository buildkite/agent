package cryptominer

import (
	"bufio"
	"path/filepath"
	"strconv"
	"strings"
)

// ProcessInfo contains information about a running process.
type ProcessInfo struct {
	PID     int
	PGID    int
	Comm    string // short name from /proc/<pid>/comm
	Cmdline string // full command line
	ExePath string // resolved path of executable
}

// ScanResult contains the results of scanning a process group for miners.
type ScanResult struct {
	Found   bool
	Matches []MatchResult
}

// scanProcesses checks a slice of ProcessInfo for cryptominer patterns.
func scanProcesses(processes []ProcessInfo) ScanResult {
	result := ScanResult{}

	for _, proc := range processes {
		// Check process name (comm)
		if suspicious, matched := IsSuspiciousBinary(proc.Comm); suspicious {
			result.Found = true
			result.Matches = append(result.Matches, MatchResult{
				PID:         proc.PID,
				ProcessName: proc.Comm,
				Cmdline:     proc.Cmdline,
				MatchType:   "binary",
				MatchDetail: matched,
			})
			continue
		}

		// Check executable path basename
		if proc.ExePath != "" {
			exeName := filepath.Base(proc.ExePath)
			if suspicious, matched := IsSuspiciousBinary(exeName); suspicious {
				result.Found = true
				result.Matches = append(result.Matches, MatchResult{
					PID:         proc.PID,
					ProcessName: exeName,
					Cmdline:     proc.Cmdline,
					MatchType:   "binary",
					MatchDetail: matched,
				})
				continue
			}
		}

		// Check command line patterns
		if proc.Cmdline != "" {
			if suspicious, matched := IsSuspiciousCmdline(proc.Cmdline); suspicious {
				result.Found = true
				result.Matches = append(result.Matches, MatchResult{
					PID:         proc.PID,
					ProcessName: proc.Comm,
					Cmdline:     proc.Cmdline,
					MatchType:   "pattern",
					MatchDetail: matched,
				})
			}
		}
	}

	return result
}

// ScanProcCmdline is a fallback for non-Linux systems that uses ps command output.
// It takes pre-parsed ps output lines in the format "PGID PID COMMAND..."
func ScanProcCmdline(lines []string, targetPGID int) (ScanResult, error) {
	result := ScanResult{}

	scanner := bufio.NewScanner(strings.NewReader(strings.Join(lines, "\n")))
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		pgid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}

		if pgid != targetPGID {
			continue
		}

		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}

		cmdline := strings.Join(fields[2:], " ")
		procName := filepath.Base(fields[2])

		// Check binary name
		if suspicious, matched := IsSuspiciousBinary(procName); suspicious {
			result.Found = true
			result.Matches = append(result.Matches, MatchResult{
				PID:         pid,
				ProcessName: procName,
				Cmdline:     cmdline,
				MatchType:   "binary",
				MatchDetail: matched,
			})
			continue
		}

		// Check command line patterns
		if suspicious, matched := IsSuspiciousCmdline(cmdline); suspicious {
			result.Found = true
			result.Matches = append(result.Matches, MatchResult{
				PID:         pid,
				ProcessName: procName,
				Cmdline:     cmdline,
				MatchType:   "pattern",
				MatchDetail: matched,
			})
		}
	}

	return result, scanner.Err()
}
