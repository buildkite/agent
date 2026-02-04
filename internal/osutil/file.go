package osutil

import (
	"fmt"
	"os"
)

// ChmodExecutable sets the executable mode/flag on a file, if not already.
func ChmodExecutable(filename string) error {
	s, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("Failed to retrieve file information of \"%s\" (%s)", filename, err)
	}
	if s.Mode()&0o100 == 0 {
		err = os.Chmod(filename, s.Mode()|0o100)
		if err != nil {
			return fmt.Errorf("Failed to mark \"%s\" as executable (%s)", filename, err)
		}
	}
	return nil
}

// FileExists returns whether or not a file exists on the filesystem. We
// consider any error returned by os.Stat to indicate that the file doesn't
// exist. We could be specific and use os.IsNotExist(err), but most other
// errors also indicate that the file isn't there (or isn't available) so we'll
// just catch them all.
func FileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}
