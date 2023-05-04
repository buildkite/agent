package agentapi

import "sync"

// lockState is really just a concurrent map.
type lockState struct {
	mu    sync.Mutex
	locks map[string]string
}

// newLockState creates a new empty lockServer.
func newLockState() *lockState {
	return &lockState{
		locks: make(map[string]string),
	}
}

// load atomically retrieves the current value for the lock.
func (s *lockState) load(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.locks[key]
}

// cas atomically attempts to swap the old value for the key for a new
// value. It reports whether the swap succeeded, returning the (new or existing)
// value.
func (s *lockState) cas(key, old, new string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locks[key] == old {
		s.locks[key] = new
		return new, true
	}
	return s.locks[key], false
}
