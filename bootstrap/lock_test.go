package bootstrap

import (
	"context"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquiringLock(t *testing.T) {
	dir, err := ioutil.TempDir("", "example")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err = acquireLock(ctx, filepath.Join(dir, "my.lock")); err != nil {
		t.Fatal(err)
	}
}

func TestAcquiringLockWithTimeout(t *testing.T) {
	dir, err := ioutil.TempDir("", "example")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	lockPath := filepath.Join(dir, "my.lock")

	// acquire a lock in another process
	cmd, err := execAcquiringLockHelperProcess(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	// wait for the above process to get a lock
	for {
		if _, err := os.Stat(lockPath); os.IsNotExist(err) {
			time.Sleep(time.Millisecond * 10)
			continue
		}
		break
	}

	defer cmd.Process.Kill()

	// acquire lock
	_, err = acquireLockWithTimeout(lockPath, time.Microsecond*5)
	if err != context.DeadlineExceeded {
		t.Fatalf("Expected DeadlineExceeded error, got %v", err)
	}
}

func execAcquiringLockHelperProcess(lockfile string) (*exec.Cmd, error) {
	cmd := exec.Command(os.Args[0], "-test.run=TestAcquiringLockHelperProcess", "--", lockfile)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}
	return cmd, cmd.Start()
}

// TestAcquiringLockHelperProcess isn't a real test. It's used as a helper process
func TestAcquiringLockHelperProcess(*testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	fileName := os.Args[len(os.Args)-1]

	log.Printf("Locking %s", fileName)
	if _, err := acquireLock(context.Background(), fileName); err != nil {
		os.Exit(1)
	}

	log.Printf("Acquired lock %s", fileName)
	c := make(chan struct{})
	<-c
}
