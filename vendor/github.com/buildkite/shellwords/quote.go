package shellwords

import (
	"runtime"
)

// Quote chooses between QuotePosix and QuoteBatch based on your operating system
func Quote(word string) string {
	if runtime.GOOS == `windows` {
		return QuoteBatch(word)
	}
	return QuotePosix(word)
}
