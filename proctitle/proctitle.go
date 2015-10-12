// +build linux,386 linux,arm linux,amd64,!cgo darwin windows freebsd

package proctitle

func Replace(title string) {
	// Only supported on Linux 64 bit :(
}
