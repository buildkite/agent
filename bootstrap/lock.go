package bootstrap

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/nightlyone/lockfile"
)

var (
	lockRetryDuration = time.Second
)

func acquireLock(ctx context.Context, path string) (*lockfile.Lockfile, error) {
	absolutePathToLock, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("Failed to find absolute path to lock \"%s\" (%v)", path, err)
	}

	lock, err := lockfile.New(absolutePathToLock)
	if err != nil {
		return nil, fmt.Errorf("Failed to create lock \"%s\" (%s)", absolutePathToLock, err)
	}

	for {
		// Keep trying the lock until we get it
		if err := lock.TryLock(); err != nil {
			if te, ok := err.(interface {
				Temporary() bool
			}); ok && te.Temporary() {
				commentf("Could not aquire lock on \"%s\" (%s)", absolutePathToLock, err)
				commentf("Trying again in %s...", lockRetryDuration)
				time.Sleep(lockRetryDuration)
			} else {
				return nil, err
			}
		} else {
			break
		}

		// Check if we've timed out
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}
	}

	return &lock, err
}

func acquireLockWithTimeout(path string, timeout time.Duration) (*lockfile.Lockfile, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return acquireLock(ctx, path)
}
