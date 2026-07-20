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
		// Wait for a byte on stdin before reading priority.
		// The parent writes this byte after postStart has applied Setpriority,
		// avoiding a race where we read priority before it's been set.
		buf := make([]byte, 1)
		if _, err := os.Stdin.Read(buf); err != nil {
			log.Fatalf("waiting for start signal: %v", err)
		}

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
