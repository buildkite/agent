// +build !linux,amd64

package agent

func SetProcTitle(title string) {
	// Only supported on Linux :(
}
