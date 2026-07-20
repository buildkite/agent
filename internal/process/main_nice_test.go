//go:build !windows

package process_test

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"syscall"
)

func init() {
	extraTestMainCases["tester-nice"] = func() {
		prio, err := syscall.Getpriority(syscall.PRIO_PROCESS, 0)
		if err != nil {
			log.Fatalf("Getpriority: %v", err)
		}
		// On Linux, getpriority returns 20 - nice (always non-negative).
		// On macOS/others, it returns the nice value directly.
		if runtime.GOOS == "linux" {
			prio = 20 - prio
		}
		fmt.Printf("nice=%d", prio)
		os.Exit(0)
	}
}
