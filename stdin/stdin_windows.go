package stdin

import "os"

func IsPipe() bool {
	// If there is no pipe, then os.Stdin.Stat() returns error on Windows
	_, err := os.Stdin.Stat()
	if err != nil {
		return false
	} else {
		return true
	}
}
