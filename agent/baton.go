package agent

import "sync"

// baton is a channel-based mutex. This allows for using it as part of a select
// statement.
type baton struct {
	mu      sync.Mutex
	holder  string
	acquire map[string]chan struct{}
}

// newBaton creates a new baton for sharing among (non-zero) values of T.
func newBaton() *baton {
	return &baton{
		acquire: make(map[string]chan struct{}),
	}
}

// HeldBy reports if the argument holds the baton.
func (b *baton) HeldBy(by string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.holder == by
}

// Acquire returns a channel that receives when the baton is acquired by the
// acquirer.
func (b *baton) Acquire(by string) <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := b.acquire[by]
	if ch == nil {
		ch = make(chan struct{})
	}

	if b.holder == "" {
		b.holder = by
		close(ch)
		delete(b.acquire, by) // in case it is in the map
		return ch
	}

	b.acquire[by] = ch
	return ch
}

// Release releases the baton, if it is held by the argument.
func (b *baton) Release(by string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.holder != by {
		return
	}

	// Attempt to pass the baton.
	for a, ch := range b.acquire {
		delete(b.acquire, a)
		select {
		case ch <- struct{}{}:
			// This acquirer has acquired the baton.
			b.holder = a
			return
		default:
			// This acquirer has stopped waiting, try another.
		}
	}
	// Nothing was waiting, nothing now holds the baton.
	b.holder = ""
}
