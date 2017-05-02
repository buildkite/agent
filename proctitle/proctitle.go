// +build linux,386 linux,arm linux,arm64 linux,amd64,!cgo darwin windows freebsd openbsd

package proctitle

func Replace(title string) {
	// Only supported on Linux 64 bit :(
}
