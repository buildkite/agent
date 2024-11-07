package file

import (
	"fmt"
	"os"
	"strconv"

	"github.com/buildkite/agent/v3/internal/shell"
)

// IsOpened returns true if the file at the given path is opened by the current process.
func IsOpened(l shell.Logger, debug bool, path string) (bool, error) {
	fdEntries, err := os.ReadDir("/dev/fd")
	if err != nil {
		return false, fmt.Errorf("failed to read /dev/fd: %w", err)
	}

	for _, fdEntry := range fdEntries {
		fd, err := strconv.ParseInt(fdEntry.Name(), 10, 64)
		if err != nil {
			if debug {
				l.Warningf("Failed to parse fd %s: %s", fd, err)
			}
			continue
		}

		if fd <= stderrFd {
			continue
		}

		fdPath, err := os.Readlink(fmt.Sprintf("/dev/fd/%d", fd))
		if err != nil {
			if debug {
				l.Warningf("Failed to readlink /dev/fd/%d: %v", fd, err)
			}
			continue
		}

		if fdPath == path {
			return true, nil
		}
	}

	return false, nil
}
