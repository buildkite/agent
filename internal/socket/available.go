//go:build unix && !windows

package socket

func Available() bool {
	return true
}
