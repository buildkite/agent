package stdin

import (
	"os"
)

func IsReadable() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	// Check we have something to read on stdin
	// See https://flaviocopes.com/go-shell-pipes/
	if fi.Mode()&os.ModeCharDevice != 0 || fi.Size() <= 0 {
		return false
	}
	return true
}
