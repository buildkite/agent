//go:build !windows

package process_test

import (
	"fmt"
	"log"
	"os"
	"syscall"
)

func init() {
	extraTestMainCases["tester-nice"] = func() {
		prio, err := syscall.Getpriority(syscall.PRIO_PROCESS, 0)
		if err != nil {
			log.Fatalf("Getpriority: %v", err)
		}
		fmt.Printf("nice=%d", prio)
		os.Exit(0)
	}
}
