// +build !linux !cgo

package agent

func SetProcTitle(title string) {
	// Only supported on Linux 64 bit :(
}
