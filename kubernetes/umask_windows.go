//go:build windows
// +build windows

package kubernetes

import (
	"errors"
)

// Umask returns an error on Windows
func Umask(mask int) (int, error) {
	return 0, errors.New("platform and architecture is not supported")
}
