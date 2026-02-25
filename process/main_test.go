package process_test

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/process"
)

// Invoked by `go test`, switch between helper and running tests based on env
func TestMain(m *testing.M) {
	switch os.Getenv("TEST_MAIN") {
	case "tester":
		for line := range strings.SplitSeq(strings.TrimSuffix(longTestOutput, "\n"), "\n") {
			fmt.Printf("%s\n", line)
			time.Sleep(time.Millisecond * 20)
		}
		os.Exit(0)

	case "output":
		fmt.Fprintf(os.Stdout, "llamas1\n") //nolint:errcheck // test helper process output
		fmt.Fprintf(os.Stderr, "alpacas1\r")   //nolint:errcheck // test helper process output
		fmt.Fprintf(os.Stdout, "llamas2\r\n") //nolint:errcheck // test helper process output
		fmt.Fprintf(os.Stderr, "alpacas2\n")  //nolint:errcheck // test helper process output
		os.Exit(0)

	// don't handle the signals so that we can detect the process was signaled
	case "tester-no-handler":
		fmt.Println("Ready")
		time.Sleep(10 * time.Second)
		os.Exit(0)

	// takes too long to handle the signals, so will be sigkilled
	case "tester-slow-handler":
		signals := make(chan os.Signal, 1)
		signal.Notify(
			signals,
			os.Interrupt,
			syscall.SIGINT,
			syscall.SIGTERM,
		)

		go func() {
			for s := range signals {
				fmt.Fprintf(os.Stdout, "received signal: %d", s) //nolint:errcheck // test helper process output
				time.Sleep(10 * time.Second)
				os.Exit(0)
			}
		}()

		fmt.Println("Ready")
		time.Sleep(15 * time.Second)
		os.Exit(0)

	case "tester-signal":
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt,
			syscall.SIGTERM,
			syscall.SIGINT,
		)
		fmt.Println("Ready")
		fmt.Printf("SIG %v", <-signals)
		os.Exit(0)

	case "tester-pgid":
		pid := syscall.Getpid()
		pgid, err := process.GetPgid(pid)
		if err != nil {
			log.Fatal(err)
		}
		if pgid != pid {
			log.Fatalf("Bad pgid, expected %d, got %d", pid, pgid)
		}
		fmt.Printf("pid %d == pgid %d", pid, pgid)
		os.Exit(0)

	default:
		os.Exit(m.Run())
	}
}
