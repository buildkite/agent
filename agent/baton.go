package agent

import "sync"

// baton is a channel-based mutex. This allows for using it as part of a select
// statement.
type baton struct {
	mu      sync.Mutex
	holder  string
	acquire map[string]chan struct{}
}

// newBaton creates a new baton for sharing among actors, each identified
// by a non-empty string.
func newBaton() *baton {
	return &baton{
		acquire: make(map[string]chan struct{}),
	}
}

// HeldBy reports if the actor specified by the argument holds the baton.
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

	// If there's an existing channel for this actor, reuse it.
	ch := b.acquire[by]
	if ch == nil {
		ch = make(chan struct{})
	}

	// If nothing holds the baton currently, assign it to the caller.
	// The caller won't be receiving on the channel until after we
	// return it, so make the channel receivable by closing it.
	if b.holder == "" {
		b.holder = by
		close(ch)
		delete(b.acquire, by) // in case it is in the map
		return ch
	}

	// Something holds the baton, so record that this actor is
	// waiting for the baton.
	b.acquire[by] = ch
	return ch
}

// Release releases the baton, if it is held by the argument.
func (b *baton) Release(by string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Only release if its the same actor, to prevent bugs due
	// to double-releasing.
	if b.holder != by {
		return
	}

	// Attempt to pass the baton to anything still waiting for it.
	for a, ch := range b.acquire {
		delete(b.acquire, a)
		select {
		case ch <- struct{}{}:
			// We were able to send a value to the channel,
			// so this actor was still waiting to receive.
			// Therefore this actor has acquired the baton.
			b.holder = a
			return
		default:
			// This actor has stopped waiting to receive,
			// so try another.
		}
	}

	// Nothing was still waiting on its channel,
	// so now nothing holds the baton.
	b.holder = ""
}
