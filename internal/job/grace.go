package job

import (
	"context"
	"time"
)

// withGracePeriod returns a context that is cancelled some time *after* the
// parent context is cancelled. In general this is not a good pattern, since it
// breaks the usual connection between context cancellations and requires an
// extra goroutine. However, we need to enforce the signal grace period from
// within the job executor for use-cases where the executor is _not_ forked from
// something else that will enforce the grace period (with SIGKILL).
func withGracePeriod(ctx context.Context, graceTimeout time.Duration) context.Context {
	gctx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	go func() {
		<-ctx.Done()
		time.Sleep(graceTimeout)
		cancel(context.DeadlineExceeded)
	}()
	return gctx
}
