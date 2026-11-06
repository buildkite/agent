package agent

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
	"github.com/google/go-cmp/cmp"
)

func TestLogStreamer(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	logger := logger.NewConsoleLogger(
		logger.NewTextPrinter(os.Stderr),
		func(c int) { t.Errorf("exit(%d)", c) },
	)

	var mu sync.Mutex
	var got []*api.Chunk
	callback := func(ctx context.Context, chunk *api.Chunk) error {
		mu.Lock()
		got = append(got, chunk)
		mu.Unlock()
		return nil
	}

	ls := NewLogStreamer(logger, callback, LogStreamerConfig{
		Concurrency:       3,
		MaxChunkSizeBytes: 10,
		MaxSizeBytes:      30,
	})

	if err := ls.Start(ctx); err != nil {
		t.Fatalf("LogStreamer.Start(ctx) = %v", err)
	}

	input := "0123456789abcdefghijklmnopqrstuvwxyz!@#$%^&*()" // 46 bytes
	if err := ls.Process(ctx, []byte(input)); err != nil {
		t.Errorf("LogStreamer.Process(ctx, %q) = %v", input, err)
	}

	ls.Stop()

	want := []*api.Chunk{
		{
			Data:     []byte("0123456789"),
			Sequence: 1,
			Offset:   0,
			Size:     10,
		},
		{
			Data:     []byte("abcdefghij"),
			Sequence: 2,
			Offset:   10,
			Size:     10,
		},
		{
			Data:     []byte("klmnopqrst"),
			Sequence: 3,
			Offset:   20,
			Size:     10,
		},
		{
			Data:     []byte("uvwxyz!@#$"),
			Sequence: 4,
			Offset:   30,
			Size:     10,
		},
		{
			Data:     []byte("%^&*()"),
			Sequence: 5,
			Offset:   40,
			Size:     6,
		},
	}

	sort.Slice(got, func(i, j int) bool {
		return got[i].Sequence < got[j].Sequence
	})

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("LogStreamer chunks diff (-got +want):\n%s", diff)
	}

	input = "¿more log after stop?"
	if err := ls.Process(ctx, []byte(input)); !errors.Is(err, errStreamerStopped) {
		t.Errorf("after Stop: LogStreamer.Process(ctx, %q) err = %v, want %v", input, err, errStreamerStopped)
	}
}

func TestLogStreamerDecodedOSC(t *testing.T) {
	const first = "\x1b]7501;state=error:id=tool:msg=T25l\a"
	const second = "\x1b]7501;state=error:id=tool:msg=VHdv\x1b\\"
	// Canceled reports bypass decoded-field redaction and must not be forwarded.
	const canceled = "\x1b]7501;state=error:msg=c2VjcmV0:x=\x18\a" +
		"\x1b]7501;state=error:msg=c2VjcmV0:x=\x1a\a"
	input := "prefix\n" + first + "\nbetween\n" + canceled + second
	var got []api.OSCEvidence
	ls := NewLogStreamer(logger.Discard, func(context.Context, *api.Chunk) error { return nil }, LogStreamerConfig{
		Concurrency: 3, MaxChunkSizeBytes: 7,
		ForwardOSC: func(_ context.Context, reports []api.OSCEvidence) { got = append(got, reports...) },
	})
	if err := ls.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := ls.Process(t.Context(), []byte(input)); err != nil {
		t.Fatal(err)
	}
	ls.Stop()
	want := []api.OSCEvidence{
		{Offset: 7, Report: api.OSCReport{State: "error", ID: "tool", Msg: "One"}},
		{Offset: uint64(7 + len(first) + len("\nbetween\n") + len(canceled)), Report: api.OSCReport{State: "error", ID: "tool", Msg: "Two"}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("decoded evidence (-want +got):\n%s", diff)
	}
}

func TestLogStreamerOSCWhileLogsDrain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var delivered int
		ls := NewLogStreamer(logger.Discard, func(context.Context, *api.Chunk) error {
			time.Sleep(8 * time.Second)
			return nil
		}, LogStreamerConfig{
			Concurrency: 1, MaxChunkSizeBytes: 65536,
			ForwardOSC: func(ctx context.Context, reports []api.OSCEvidence) {
				select {
				case <-time.After(6 * time.Second):
					delivered += len(reports)
				case <-ctx.Done():
				}
			},
		})
		if err := ls.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		if err := ls.Process(t.Context(), []byte(strings.Repeat("\x1b]7501;state=error\a", 32))); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		ls.Stop()
		if delivered != 32 {
			t.Errorf("delivered %d reports; evidence must keep draining during required log uploads", delivered)
		}
	})
}

func TestLogStreamerOSCShutdownBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ls := NewLogStreamer(logger.Discard, func(context.Context, *api.Chunk) error { return nil }, LogStreamerConfig{
			Concurrency: 1, MaxChunkSizeBytes: 65536,
			ForwardOSC: func(ctx context.Context, _ []api.OSCEvidence) { <-ctx.Done() },
		})
		if err := ls.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		for range 64 {
			if err := ls.Process(t.Context(), []byte("\x1b]7501;state=error\a")); err != nil {
				t.Fatal(err)
			}
		}
		synctest.Wait()
		start := time.Now()
		ls.Stop()
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Errorf("shutdown took %s; evidence must not extend it beyond 5s", elapsed)
		}
	})
}
