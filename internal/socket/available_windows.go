//go:build windows

package socket

import (
	"strconv"

	"golang.org/x/sys/windows/registry"
)

// Available returns true if the job api is available on this machine, which is determined by the OS and OS version running the agent
// The job API uses unix domain sockets, which are only available on unix machines, and on windows machines after build 17063
// So:
// On all unices, this function will return true
// On windows, it will return true if and only if the build is after 17063 (the first build to support unix sockets)
func Available() bool {
	return isAfterBuild17063()
}

// isAfterBuild17063 returns true if the current build (of windows, this file is only compiled for windows) is after 17063
// stolen from: https://github.com/golang/go/blob/76c45877c9e72ccc84db787dc08299e0182e0efb/src/net/unixsock_windows_test.go#L17
func isAfterBuild17063() bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.READ)
	if err != nil {
		return false
	}
	defer k.Close()

	s, _, err := k.GetStringValue("CurrentBuild")
	if err != nil {
		return false
	}
	ver, err := strconv.Atoi(s)
	if err != nil {
		return false
	}
	return ver >= 17063
}
