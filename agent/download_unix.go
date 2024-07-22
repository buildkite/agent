//go:build unix

package agent

import (
	"os"

	"golang.org/x/sys/unix"
)

func init() {
	// Can't read the current umask(2) without changing it.
	umask = os.FileMode(unix.Umask(int(umask)))
	unix.Umask(int(umask))
}
