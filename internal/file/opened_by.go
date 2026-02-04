package file

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/buildkite/agent/v3/internal/shell"
)

const stderrFd = 2

var (
	ErrFileNotOpen = errors.New("file not open, or the procces that opened it can't be found")
	numeric        = regexp.MustCompile("^[0-9]+$")
)

// OpenedBy attempts to find the executable that opened the given file.
func OpenedBy(l shell.Logger, debug bool, path string) (string, error) {
	pidEntries, err := os.ReadDir("/proc")
	if err != nil {
		return "", fmt.Errorf("failed to read /proc: %w", err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	for _, p := range pidEntries {
		pid := p.Name()

		if !numeric.MatchString(pid) || !openedByPid(l, debug, absPath, pid) {
			continue
		}

		// /proc/<pid>/exe is a symlink to the executable
		exe, err := os.Readlink(fmt.Sprintf("/proc/%s/exe", pid))
		if err != nil {
			if debug {
				l.Warningf("Failed to read executable for pid %s: %v", pid, err)
			}
			continue
		}

		return exe, nil
	}

	return "", ErrFileNotOpen
}

func openedByPid(l shell.Logger, debug bool, absPath, pid string) bool {
	dirEntries, err := os.ReadDir(fmt.Sprintf("/proc/%s/fd", pid))
	if err != nil {
		if debug {
			l.Warningf("Failed to read /proc/%s/fd: %v", pid, err)
		}
		// the process has gone away, or we don't have permission to read it, ignore and move on
		return false
	}

	for _, dirEntry := range dirEntries {
		fd, err := strconv.ParseInt(dirEntry.Name(), 10, 64)
		if err != nil {
			if debug {
				l.Warningf("Failed to parse fd %s: %s", fd, err)
			}
			continue
		}

		// 0 = stdin, 1 = stdout, 2 = stderr
		if fd <= stderrFd {
			continue
		}

		fPath, err := os.Readlink(fmt.Sprintf("/proc/%s/fd/%s", pid, dirEntry.Name()))
		if err != nil {
			if debug {
				l.Warningf("Failed to read link for fd", "error", err)
			}
			continue
		}

		if fPath == absPath {
			return true
		}
	}

	return false
}
