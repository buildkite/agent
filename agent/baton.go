package agent

// Baton coordinates exclusive access between multiple BatonHolders.
// At most one holder has the baton at any time. Holders interact through
// channels, making the baton composable with select statements.
type Baton struct {
	ch chan struct{}
}

// NewBaton creates a new Baton. No one holds it initially.
func NewBaton() *Baton {
	return &Baton{ch: make(chan struct{}, 1)}
}

// Holder returns a BatonHolder that does not initially hold the baton.
func (b *Baton) Holder() *BatonHolder {
	return &BatonHolder{ch: b.ch}
}

// BatonHolder is a per-consumer handle to a Baton. It tracks whether this
// consumer currently holds the baton and provides idempotent Release.
type BatonHolder struct {
	ch   chan struct{}
	held bool
}

// Acquire returns a channel that receives when the baton is available.
//
// Ideally, receiving from the channel and updating the holder's state would
// happen atomically. However, Go's select statement requires a bare channel
// for case expressions, so the caller must explicitly call Acquired after
// a successful receive:
//
//	select {
//	case <-holder.Acquire():
//	    holder.Acquired()
//	    defer holder.Release()
//	case <-ctx.Done():
//	}
func (h *BatonHolder) Acquire() <-chan struct{} {
	return h.ch
}

// Acquired marks this holder as holding the baton.
// Must be called after successfully receiving from Acquire.
func (h *BatonHolder) Acquired() {
	h.held = true
}

// Held reports whether this holder currently holds the baton.
func (h *BatonHolder) Held() bool {
	return h.held
}

// Release makes the baton available for another holder to acquire.
// It is idempotent — calling Release when not holding is a no-op.
func (h *BatonHolder) Release() {
	if !h.held {
		return
	}
	h.held = false
	h.ch <- struct{}{}
}
