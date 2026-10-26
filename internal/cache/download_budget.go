package cache

import (
	"context"
	"errors"
	"sync"
	"time"
)

var errDownloadTimeout = errors.New("cache download timeout exceeded")

// downloadBudget counts wall time while any download is active. Overlapping
// downloads share a deadline; time between downloads does not use the allowance.
type downloadBudget struct {
	mu        sync.Mutex
	remaining time.Duration
	started   time.Time
	active    int
}

// start returns a download-only context and a release function. Release only
// after the downloader has joined its writers, not merely after cancellation.
// A nil budget disables the timeout without detaching from job cancellation.
func (b *downloadBudget) start(ctx context.Context) (context.Context, context.CancelFunc) {
	if b == nil {
		return ctx, func() {}
	}
	b.mu.Lock()
	if b.active == 0 {
		b.started = time.Now()
	}
	b.active++
	deadline := b.started.Add(b.remaining)
	b.mu.Unlock()
	ctx, cancel := context.WithDeadlineCause(ctx, deadline, errDownloadTimeout)
	return ctx, func() {
		cancel()
		b.mu.Lock()
		defer b.mu.Unlock()
		b.active--
		if b.active == 0 {
			b.remaining -= time.Since(b.started)
		}
	}
}
