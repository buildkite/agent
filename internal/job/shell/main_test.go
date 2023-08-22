package shell_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/job/shell"
)

var errMissingFilename = errors.New("missing file name")

func TestMain(m *testing.M) {
	runHelper, found := os.LookupEnv("TEST_MAIN_WANT_HELPER_PROCESS")
	if !found || runHelper != "1" {
		os.Exit(m.Run())
	}

	if err := acquiringLockHelperProcess(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v", err)
		os.Exit(1)
	}
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

	log.Printf("Locking %s", fileName)
	if _, err := sh.LockFile(context.Background(), fileName, 10*time.Second); err != nil {
		return fmt.Errorf("sh.LockFile(%q) error = %w", fileName, err)
	}

	log.Printf("Acquired lock %s", fileName)
	// sleep forever, but keep the main goroutine busy
	c := make(chan struct{}, 1)
	for {
		select {
		case <-c:
		case c <- struct{}{}:
		}
	}
}
