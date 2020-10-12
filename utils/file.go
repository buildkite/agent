package utils

import (
	"os"
)

// FileExists returns whether or not a file exists on the filesystem. We
// consider any error returned by os.Stat to indicate that the file doesn't
// exist. We could be specific and use os.IsNotExist(err), but most other
// errors also indicate that the file isn't there (or isn't available) so we'll
// just catch them all.
func FileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}
