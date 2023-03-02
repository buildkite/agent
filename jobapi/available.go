//go:build unix && !windows

package jobapi

func Available() bool {
	return true
}
