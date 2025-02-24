package job

import (
	"context"
	"time"
)

// WithGracePeriod returns a context that is cancelled some time *after* the
// parent context is cancelled. In general this is not a good pattern, since it
// breaks the usual connection between context cancellations and requires an
// extra goroutine. However, we need to enforce the signal grace period from
// within the job executor for use-cases where the executor is _not_ forked from
// something else that will enforce the grace period (with SIGKILL).
func WithGracePeriod(ctx context.Context, graceTimeout time.Duration) (context.Context, context.CancelFunc) {
	gctx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	go func() {
		<-ctx.Done()
		select {
		case <-time.After(graceTimeout):
			cancel(context.DeadlineExceeded)

		case <-gctx.Done():
			// This can happen if the caller called the cancel func.
		}
	}()
	return gctx, func() { cancel(context.Canceled) }
}
