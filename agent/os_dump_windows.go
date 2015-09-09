package agent

import (
	"fmt"
	"syscall"
)

func OSDump() (string, error) {
	dll := syscall.MustLoadDLL("kernel32.dll")
	p := dll.MustFindProc("GetVersion")
	v, _, _ := p.Call()

	return fmt.Sprintf("Windows version %d.%d (Build %d)\n", byte(v), uint8(v>>8), uint16(v>>16)), nil
}
