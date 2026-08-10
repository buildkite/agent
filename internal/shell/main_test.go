package shell_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/shell"
)

var errMissingFilename = errors.New("missing file name")

func TestMain(m *testing.M) {
	if os.Getenv("TEST_MAIN_CONTEXT_CANCEL_READY") == "1" {
		fmt.Println("ready")
		for {
			time.Sleep(time.Hour)
		}
	}

	// leakStdoutGrandchild must be checked before leakStdout: the grandchild
	// inherits the parent's environment (which still carries the leak-stdout
	// marker), so keying the grandchild branch first prevents infinite respawn.
	if os.Getenv("TEST_MAIN_LEAK_STDOUT_GRANDCHILD") == "1" {
		// Hold the inherited stdout/stderr write-end open well past the
		// (shortened, in tests) WaitDelay so the leaked-pipe condition is
		// exercised, then exit. It is killed via the shell's process group in
		// the test's cleanup.
		time.Sleep(60 * time.Second)
		os.Exit(0)
	}

	if os.Getenv("TEST_MAIN_LEAK_STDOUT") == "1" {
		leakStdoutHelperProcess()
		return // leakStdoutHelperProcess calls os.Exit
	}

	runHelper, found := os.LookupEnv("TEST_MAIN_WANT_HELPER_PROCESS")
	if !found || runHelper != "1" {
		os.Exit(m.Run())
	}

	if err := acquiringLockHelperProcess(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v", err)
		os.Exit(1)
	}
}

// leakStdoutHelperProcess mimics a hook whose child backgrounds a grandchild
// that inherits stdout/stderr and lives on after this process exits cleanly.
// The grandchild keeps the write-end of os/exec's internal output pipe open, so
// the parent's Cmd.Wait copy goroutine never sees EOF and WaitDelay elapses —
// the exact condition that made os/exec return exec.ErrWaitDelay for an
// otherwise-clean exit.
func leakStdoutHelperProcess() {
	grandchild := exec.Command(os.Args[0])
	grandchild.Env = append(os.Environ(), "TEST_MAIN_LEAK_STDOUT_GRANDCHILD=1")
	grandchild.Stdout = os.Stdout
	grandchild.Stderr = os.Stderr
	if err := grandchild.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start grandchild: %v", err)
		os.Exit(1)
	}
	fmt.Println("parent-exiting")
	os.Exit(0)
}

func acquiringLockHelperProcess() error {
	if len(os.Args) < 2 {
		return errMissingFilename
	}

	fileName := os.Args[len(os.Args)-1]

	sh, err := shell.New()
	if err != nil {
		return err
	}
	sh.Logger = shell.DiscardLogger

	log.Printf("🔓 Locking %s forever...", fileName)
	// This runs in a helper process outside a test function, so there is no testing.T for t.Context.
	ctx, canc := context.WithTimeout(context.Background(), 10*time.Second)
	defer canc()
	if _, err := sh.LockFile(ctx, fileName); err != nil {
		return fmt.Errorf("sh.LockFile(%q) error = %w", fileName, err)
	}

	log.Printf("🔒 Acquired lock %s", fileName)

	// sleep forever, but keep the main goroutine busy
	for {
		time.Sleep(1 * time.Second)
	}
}
