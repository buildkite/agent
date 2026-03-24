package agent

import "sync"

// baton is a channel-based mutex. This allows for using it as part of a select
// statement.
type baton struct {
	mu     sync.Mutex
	holder string
	ch     chan struct{}
}

// newBaton creates a new baton for sharing among actors, each identified
// by a non-empty string. The baton is initially not held by anything.
func newBaton() *baton {
	b := &baton{
		ch: make(chan struct{}, 1),
	}
	b.ch <- struct{}{}
	return b
}

// HeldBy reports if the actor specified by the argument holds the baton.
func (b *baton) HeldBy(by string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.holder == by
}

// Acquire returns a channel that receives when the baton is acquired by the
// caller.
// Be sure to call [baton.Acquired] after receiving from the channel, e.g.
//
//		select {
//		case <-bat.Acquire():
//			bat.Acquired("me")
//			defer bat.Release("me")
//	 	...
//	 	}
func (b *baton) Acquire() <-chan struct{} {
	// b.ch should never change, so no need to lock around it
	return b.ch
}

// Acquired must be called by the actor that successfully acquired the baton
// immediately after acquiring it.
// It panics if the baton is already marked as held.
// It is necessary to separate this from [baton.Acquire] because it is practically
// impossible to reliably and atomically pass the baton and record the new holder
// at the same time without deadlocks.
func (b *baton) Acquired(by string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.holder != "" {
		// panic is not ideal for a few reasons (a fatal log might be better),
		// but keeps baton focused on being a concurrency primitive. As long as
		// the panic reaches the Go runtime, Go will give us a traceback and exit.
		panic("baton already held by " + b.holder)
	}
	b.holder = by
}

// Release releases the baton, if it is held by the argument.
func (b *baton) Release(by string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.holder != by {
		return
	}
	b.holder = ""
	b.ch <- struct{}{}
}
