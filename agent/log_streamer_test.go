package agent

import (
	"context"
	"errors"
	"os"
	"sort"
	"sync"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/google/go-cmp/cmp"
)

func TestLogStreamer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

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
