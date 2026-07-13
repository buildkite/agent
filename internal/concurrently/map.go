// Package concurrently has helpers for writing concurrent code.
package concurrently

import (
	"context"
	"runtime"
	"sync"
)

// Option funcs alter how the concurrent algorithms behave.
type Option func(*config)

// WithWorkerCount overrides the default worker count for the operation.
// If n ≤ 0, it uses the default worker count.
func WithWorkerCount(n int) Option {
	if n <= 0 {
		return func(c *config) { c.workerCount = nil }
	}
	return func(c *config) { c.workerCount = &n }
}

type config struct {
	workerCount *int
}

// Map transforms an input slice into an output slice via a function f.
// It calls f as concurrently as reasonably possible (up to GOMAXPROCS by
// default).
func Map[In, Out any, InS ~[]In](ctx context.Context, in InS, f func(context.Context, int, In) (Out, error), opts ...Option) ([]Out, error) {
	// Short circuits for len(in) ≤ 1
	switch len(in) {
	case 0:
		return nil, nil
	case 1:
		o, err := f(ctx, 0, in[0])
		return []Out{o}, err
	}

	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}
	workerCount := min(len(in), runtime.GOMAXPROCS(0))
	if cfg.workerCount != nil {
		workerCount = *cfg.workerCount
	}

	// Lock around concurrent accesses to out, in case of misalignment
	// (suppose Out = byte, then each write writes 7 other elements on a 64-bit
	// machine).
	var outMu sync.Mutex
	out := make([]Out, len(in))
	type workUnit struct {
		in  In
		idx int
	}
	workCh := make(chan workUnit)
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	var wg sync.WaitGroup
	for range workerCount {
		wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case work, open := <-workCh:
					if !open {
						return
					}
					o, err := f(ctx, work.idx, work.in)
					if err != nil {
						cancel(err)
						return
					}
					outMu.Lock()
					out[work.idx] = o
					outMu.Unlock()
				}
			}
		})
	}
	for i, x := range in {
		select {
		case <-ctx.Done():
			return out, context.Cause(ctx)
		case workCh <- workUnit{in: x, idx: i}:
			// continue
		}
	}
	close(workCh)
	wg.Wait()

	return out, context.Cause(ctx)
}
