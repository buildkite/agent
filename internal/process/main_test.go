package process_test

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/process"
)

var extraTestMainCases = map[string]func(){}

// Invoked by `go test`, switch between helper and running tests based on env
func TestMain(m *testing.M) {
	if fn, ok := extraTestMainCases[os.Getenv("TEST_MAIN")]; ok {
		fn()
		return // fn is expected to call os.Exit
	}

	switch os.Getenv("TEST_MAIN") {
	case "tester":
		for line := range strings.SplitSeq(strings.TrimSuffix(longTestOutput, "\n"), "\n") {
			fmt.Printf("%s\n", line)
			time.Sleep(time.Millisecond * 20)
		}
		os.Exit(0)

	case "output":
		_, _ = fmt.Fprintf(os.Stdout, "llamas1\n")
		_, _ = fmt.Fprintf(os.Stderr, "alpacas1\r")
		_, _ = fmt.Fprintf(os.Stdout, "llamas2\r\n")
		_, _ = fmt.Fprintf(os.Stderr, "alpacas2\n")
		os.Exit(0)

	case "output-slow-exit":
		_, _ = fmt.Fprintf(os.Stdout, "llamas1\n")
		_, _ = fmt.Fprintf(os.Stderr, "alpacas1\r")
		_, _ = fmt.Fprintf(os.Stdout, "llamas2\r\n")
		_, _ = fmt.Fprintf(os.Stderr, "alpacas2\n")
		time.Sleep(100 * time.Millisecond)
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
				_, _ = fmt.Fprintf(os.Stdout, "received signal: %d", s)
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

	// leak-stdout spawns a grandchild that inherits our stdout/stderr (the
	// write-end of the OS pipe that os/exec created for the parent, since the
	// parent's Stdout is an *io.PipeWriter rather than an *os.File) and lives on
	// after we exit immediately. The grandchild keeps that pipe write-end open,
	// so the parent's Cmd.Wait copy goroutine never sees EOF. This reproduces
	// the agent hook-hang from the CI goroutine dump.
	case "leak-stdout":
		grandchild := exec.Command(os.Args[0])
		grandchild.Env = append(os.Environ(), "TEST_MAIN=leak-grandchild")
		grandchild.Stdout = os.Stdout
		grandchild.Stderr = os.Stderr
		if err := grandchild.Start(); err != nil {
			log.Fatalf("failed to start grandchild: %v", err)
		}
		fmt.Println("parent-exiting")
		os.Exit(0)

	// leak-grandchild holds the inherited stdout/stderr open well beyond the
	// parent's WaitDelay so the leaked-pipe condition is exercised.
	case "leak-grandchild":
		time.Sleep(60 * time.Second)
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
