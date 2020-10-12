package utils

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
	if s.Mode()&0100 == 0 {
		err = os.Chmod(filename, s.Mode()|0100)
		if err != nil {
			return fmt.Errorf("Failed to mark \"%s\" as executable (%s)", filename, err)
		}
	}
	return nil
}
