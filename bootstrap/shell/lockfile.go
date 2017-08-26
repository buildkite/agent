package shell

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

// LockFile provides a helper for cross-platform file locking
func LockFile(sh *Shell, path string) (*lockfile.Lockfile, error) {
	return LockFileWithContext(sh.ctx, sh, path)
}

// LockFileWithContext provides a helper for cross-platform file locking with a specific context.Context
func LockFileWithContext(ctx context.Context, sh *Shell, path string) (*lockfile.Lockfile, error) {
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
				sh.Commentf("Could not aquire lock on \"%s\" (%s)", absolutePathToLock, err)
				sh.Commentf("Trying again in %s...", lockRetryDuration)
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

// LockFileWithTimeout provides a helper for cross-platform file locking with a timeout
func LockFileWithTimeout(sh *Shell, path string, timeout time.Duration) (*lockfile.Lockfile, error) {
	ctx, cancel := context.WithTimeout(sh.ctx, timeout)
	defer cancel()

	return LockFileWithContext(ctx, sh, path)
}
