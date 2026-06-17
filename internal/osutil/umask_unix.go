//go:build unix

package osutil

import (
	"os"

	"golang.org/x/sys/unix"
)

func init() {
	// Can't read the current umask(2) without changing it.
	Umask = os.FileMode(unix.Umask(int(Umask)))
	unix.Umask(int(Umask))
}
