package stdin

import (
	"os"
)

// This is a tricky problem and we have gone through several iterations before
// settling on something that works well for recent golang across windows,
// linux and macos.

func IsReadable() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}

	// Named pipes on unix/linux indicate a readable stdin, but might not have size yet
	if fi.Mode()&os.ModeNamedPipe != 0 {
		return true
	}

	return fi.Size() > 0
}
