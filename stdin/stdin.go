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

	// Character devices in Linux/Unix are unbuffered devices that have
	// direct access to underlying hardware and don't allow reading single characters at a time
	if (fi.Mode() & os.ModeCharDevice) == os.ModeCharDevice {
		return false
	}

	return true
}
