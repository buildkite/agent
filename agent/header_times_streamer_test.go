package agent

import (
	"context"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/logger"
)

func TestHeaderTimesStreamerScanAfterStopDoesNotPanic(t *testing.T) {
	t.Parallel()

	h := newHeaderTimesStreamer(logger.Discard, func(context.Context, int, int, map[string]string) {})

	runDone := make(chan struct{})
	go func() {
		h.Run(t.Context())
		close(runDone)
	}()

	deadline := time.After(500 * time.Millisecond)
	for {
		h.streamingMu.Lock()
		streaming := h.streaming
		h.streamingMu.Unlock()

		if streaming {
			break
		}

		select {
		case <-deadline:
			t.Fatal("timed out waiting for header times streamer to start")
		default:
			time.Sleep(1 * time.Millisecond)
		}
	}

	stopDone := make(chan struct{})
	go func() {
		h.Stop()
		close(stopDone)
	}()

	select {
	case <-stopDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for header times streamer to stop")
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Scan panicked after Stop: %v", r)
		}
	}()

	if got := h.Scan("--- a header"); !got {
		t.Fatalf("Scan() = %t, want true", got)
	}

	select {
	case <-runDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for header times streamer run loop to exit")
	}
}
