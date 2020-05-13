// Package lockfile provides a thread and process-safe lock.
package lockfile

import (
	"math/rand"
	"sync"
	"time"

	"github.com/nightlyone/lockfile"
	"github.com/pkg/errors"
)

// ErrAlreadyLocked is returned when the lock we're trying to lock is already
// locked.
var ErrAlreadyLocked = errors.New("this lock is already held within this process")

// ErrNotLocked is returned when the lock we're trying to unlock is not locked.
var ErrNotLocked = errors.New("unlock called on unlocked lock")

// ErrNotOurLock is returned when the lock we're trying to unlock is locked by
// another thread.
var ErrNotOurLock = errors.New("this lock is being held within the process")

// lockRegistry guards within the process against concurrent lock acquisition.
type lockRegistry struct {
	*sync.Mutex

	// Set of paths for locks that are being held within this process.
	paths map[string]int64
}

// newRegistry creates a new lockRegistry.
func newRegistry() *lockRegistry {
	return &lockRegistry{
		Mutex: &sync.Mutex{},
		paths: make(map[string]int64),
	}
}

// registry coordinates file locking within the process.
var registry = newRegistry()

// LockFile is a thread and process-safe file lock. It combines an OS-level
// file lock with an in-process mutex to provide a lock that will function
// safely across and within processes.
type LockFile struct {
	id       int64
	fileLock lockfile.Lockfile
	path     string
}

// New creates a new LockFile.
func New(path string) (*LockFile, error) {
	f, err := lockfile.New(path)
	if err != nil {
		return nil, errors.Wrap(err, "other process holding lock")
	}

	return &LockFile{
		id:       rand.Int63(),
		fileLock: f,
		path:     path,
	}, nil
}

// TryLock attempts to acquire the lock.
func (l *LockFile) TryLock() error {
	// NOTE: To prevent deadlocks, always lock the registry (thread) lock
	// before the file (process) lock.
	// Releasing must always be ordered file (process) then registry (thread).
	registry.Lock()
	defer registry.Unlock()

	// If another thread has the lock, don't allow it to be acquired.
	if _, ok := registry.paths[l.path]; ok {
		return ErrAlreadyLocked
	}

	// Attempt to acquire the inter-process lock.
	if err := l.fileLock.TryLock(); err != nil {
		return errors.Wrap(err, "could not acquire file lock")
	}

	// At this point, we have the exclusive lock within the process on all
	// paths, and a lock across all processes for our given path. Persist
	// within the process that we own this lock, and return a reference to the
	// lock.
	registry.paths[l.path] = l.id

	return nil
}

// Unlock attempts to relinquish the lock.
func (l *LockFile) Unlock() error {
	registry.Lock()
	defer registry.Unlock()

	// Check our in-process state before attempting to unlock inter-process
	// lock.
	id, ok := registry.paths[l.path]
	if !ok {
		return ErrNotLocked
	}

	// Ensure the lock is actually owned by this lock
	if id != l.id {
		return ErrNotOurLock
	}

	// Relinquish our hold on the inter-process lock. This must be relinquished
	// before the in-process lock to avoid deadlocks in case of failure.
	if err := l.fileLock.Unlock(); err != nil {
		return errors.Wrap(err, "failed to relinquish file lock")
	}

	// Relinquish our hold on the in-process lock.
	delete(registry.paths, l.path)
	return nil
}

func init() {
	rand.Seed(int64(time.Now().Nanosecond()))
}
