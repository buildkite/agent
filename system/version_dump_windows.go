// +build windows

package system

import (
	"fmt"
	"syscall"

	"github.com/buildkite/agent/logger"
)

// VersionDump returns a string representing the operating system
func VersionDump(_ *logger.Logger) (string, error) {
	dll := syscall.MustLoadDLL("kernel32.dll")
	p := dll.MustFindProc("GetVersion")
	v, _, _ := p.Call()

	return fmt.Sprintf("Windows version %d.%d (Build %d)\n", byte(v), uint8(v>>8), uint16(v>>16)), nil
}
