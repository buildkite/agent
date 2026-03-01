package agent

import "sync"

// baton is a channel-based mutex. This allows for using it as part of a select
// statement.
type baton[T any] struct {
	mu      sync.Mutex
	holder  *T
	acquire map[*T]chan struct{}
}

// newBaton creates a new baton for sharing among instances of T. If initial is
// nil, the baton is available to the first caller of [Acquire], otherwise
// it is treated as already acquired (by the argument, which must still
// [Release] it before another caller can acquire it).
func newBaton[T any](initial *T) *baton[T] {
	b := &baton[T]{
		holder:  initial,
		acquire: make(map[*T]chan struct{}),
	}
	return b
}

// Holder returns the current baton holder.
func (b *baton[T]) Holder() *T {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.holder
}

// Acquire returns a channel that receives when the baton is acquired by the
// acquirer.
func (b *baton[T]) Acquire(by *T) <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := b.acquire[by]
	if ch == nil {
		ch = make(chan struct{})
	}

	if b.holder == nil {
		b.holder = by
		close(ch)
		delete(b.acquire, by) // in case it is in the map
		return ch
	}

	b.acquire[by] = ch
	return ch
}

// Release releases the baton, if it is held.
func (b *baton[T]) Release() {
	b.mu.Lock()
	defer b.mu.Unlock()

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
	// Nothing was waiting
	b.holder = nil
}
