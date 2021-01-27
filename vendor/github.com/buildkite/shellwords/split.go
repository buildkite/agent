package shellwords

import (
	"runtime"
)

// Split chooses between SplitPosix and SplitBatch based on your operating system
func Split(line string) ([]string, error) {
	if runtime.GOOS == `windows` {
		return SplitBatch(line)
	}
	return SplitPosix(line)
}
