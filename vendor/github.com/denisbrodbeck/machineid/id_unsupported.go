// +build !freebsd,!netbsd,!openbsd,!dragonfly,!windows,!linux,!darwin

package machineid

import (
	"errors"
)

func machineID() (string, error) {
	return "", errors.New("unsupported platform")
}

