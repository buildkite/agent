//go:build unix

package job

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// hardRemoveAll tries very hard to remove all items from the directory at path.
// In addition to calling os.RemoveAll, it fixes missing +x bits on directories.
func hardRemoveAll(path string) error {
	for {
		err := os.RemoveAll(path)
		if err == nil { // If os.RemoveAll worked, then exit early.
			return nil
		}
		// os.RemoveAll documents its only non-nil error as *os.PathError.
		pathErr, ok := err.(*os.PathError)
		if !ok {
			return err
		}

		// Did we not have permission to open something within a directory?
		if pathErr.Err != unix.EACCES {
			return err
		}
		dir := filepath.Dir(pathErr.Path)

		// Check that the EACCES was caused by mode on the directory.
		// (Note that this is a TOCTOU race, but we're not changing
		// owner uid/gid, and if something else is concurrently writing
		// files they can probably chmod +wx their files themselves)
		di, statErr := os.Lstat(dir)
		if statErr != nil {
			return statErr
		}
		if !di.IsDir() {
			return err
		}
		if unix.Faccessat(0, dir, unix.W_OK|unix.X_OK, unix.AT_EACCESS) != unix.EACCES {
			// Some other failure?
			return err
		}
		// Try to fix it with chmod +x dir
		if err := os.Chmod(dir, 0o777); err != nil {
			return err
		}
		// Now retry os.RemoveAll.
	}
}
