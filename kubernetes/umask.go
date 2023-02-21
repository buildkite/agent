//go:build !windows
// +build !windows

package kubernetes

import (
	"golang.org/x/sys/unix"
)

// Umask is a wrapper for `unix.Umask()` on non-Windows platforms
func Umask(mask int) (old int, err error) {
	return unix.Umask(mask), nil
}
