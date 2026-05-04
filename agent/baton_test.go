package agent

import (
	"math/rand/v2"
	"sync"
	"testing"
	"time"
)

func TestBaton_NoDroppedBatonDeadlock(t *testing.T) {
	t.Parallel()

	bat := newBaton()

	actor := func(n string) func() {
		return func() {
			time.Sleep(rand.N(1 * time.Microsecond))
			<-bat.Acquire()
			bat.Acquired(n)
			time.Sleep(rand.N(1 * time.Microsecond))
			bat.Release(n)
		}
	}

	done := make(chan struct{})

	go func() {
		for range 10000 {
			var wg sync.WaitGroup
			wg.Go(actor("a"))
			wg.Go(actor("b"))
			wg.Wait()
		}
		close(done)
	}()

	select {
	case <-done:
		// It probably doesn't deadlock that way
	case <-time.After(10 * time.Second):
		t.Errorf("Repeated baton.Acquire/Release failed to progress, possible deadlock")
	}
}
