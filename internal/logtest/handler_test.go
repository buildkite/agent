package logtest

import (
	"fmt"
	"log/slog"
	"sync"
	"testing"
)

func TestHandlerConcurrentLogging(t *testing.T) {
	t.Parallel()

	log, handler := NewLogger()
	const writers = 20
	const recordsPerWriter = 50

	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			for j := range recordsPerWriter {
				log.Info(fmt.Sprintf("%d:%d", i, j))
			}
		})
	}
	wg.Wait()

	if got, want := len(handler.Records()), writers*recordsPerWriter; got != want {
		t.Errorf("len(handler.Records()) = %d, want %d", got, want)
	}
}

func TestHandlerCapturesLoggerAttrsAndGroups(t *testing.T) {
	t.Parallel()

	log, handler := NewLogger()
	log.With("component", "agent").WithGroup("request").Info("sent", slog.Int("status", 200))

	records := handler.Records()
	if got, want := len(records), 1; got != want {
		t.Fatalf("len(handler.Records()) = %d, want %d", got, want)
	}
	attrs := map[string]slog.Value{}
	records[0].Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value
		return true
	})
	if got, want := attrs["component"].String(), "agent"; got != want {
		t.Errorf("component = %q, want %q", got, want)
	}
	request := attrs["request"]
	if request.Kind() != slog.KindGroup {
		t.Fatalf("request.Kind() = %v, want %v", request.Kind(), slog.KindGroup)
	}
	group := request.Group()
	if got, want := len(group), 1; got != want {
		t.Fatalf("len(request.Group()) = %d, want %d", got, want)
	}
	if got, want := group[0], slog.Int("status", 200); !got.Equal(want) {
		t.Errorf("request.Group()[0] = %v, want %v", got, want)
	}
}
