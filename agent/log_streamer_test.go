package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/internal/process"
	"github.com/buildkite/agent/v4/internal/redact"
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

func TestLogStreamerProgramStatus(t *testing.T) {
	t.Parallel()
	// Exercise the writers upstream of the uploader, including a redaction that
	// changes the byte length before the first report.
	var log bytes.Buffer
	timestamper := process.NewTimestamper(&log, func(time.Time) string { return "[timestamp]" }, time.Hour)
	redactor := redact.New(timestamper, []string{"secret"})
	writer := process.NewProgramStatusWriter(redactor)
	input := "☃ secret\n\x1b]7501;state=working:id=tests:progress=0:app=runner:msg=c2VjcmV0:title=VGVzdHM=\a" +
		"\x1b]7501;state=blocked:id=tests:kind=auth:progress=37\a" +
		"\x1b]7501;state=error:id=tests:progress=99:kind=auth\a" +
		"\x1b]7501;state=clear\a"
	if _, err := writer.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := redactor.Flush(); err != nil {
		t.Fatal(err)
	}
	data := log.Bytes()
	if bytes.Contains(data, []byte("secret")) || !bytes.HasPrefix(data, []byte("[timestamp]☃ [REDACTED]\n")) {
		t.Fatalf("log was not timestamped and redacted: %q", data)
	}
	reports := []string{
		"\x1b]7501;state=working:id=tests:progress=0:app=runner\x1b\\",
		"\x1b]7501;state=blocked:id=tests:kind=auth:progress=37\x1b\\",
		"\x1b]7501;state=error:id=tests\x1b\\",
		"\x1b]7501;state=clear\x1b\\",
	}
	records := []string{
		`{"id":"tests","state":"working","app":"runner","progress":0}`,
		`{"id":"tests","state":"blocked","kind":"auth","progress":37}`,
		`{"id":"tests","state":"error"}`,
		`{"id":"","state":"clear"}`,
	}
	for _, enabled := range []bool{false, true} {
		for _, chunkSize := range []uint64{1, 17, uint64(len(data))} {
			t.Run(fmt.Sprintf("enabled=%t/chunk=%d", enabled, chunkSize), func(t *testing.T) {
				ctx := t.Context()
				if enabled {
					ctx, _ = experiments.Enable(ctx, experiments.ProgramStatus)
				}
				var mu sync.Mutex
				var chunks []*api.Chunk
				ls := NewLogStreamer(logger.Discard, func(_ context.Context, chunk *api.Chunk) error {
					mu.Lock()
					defer mu.Unlock()
					chunks = append(chunks, chunk)
					return nil
				}, LogStreamerConfig{Concurrency: 3, MaxChunkSizeBytes: chunkSize})
				if err := ls.Start(ctx); err != nil {
					t.Fatal(err)
				}
				// Also split across Process calls, inside the first report.
				split := bytes.Index(data, []byte(reports[0])) + 9
				for _, part := range [][]byte{data[:split], data[split:]} {
					if err := ls.Process(ctx, part); err != nil {
						t.Fatal(err)
					}
				}
				ls.Stop()
				sort.Slice(chunks, func(i, j int) bool { return chunks[i].Sequence < chunks[j].Sequence })
				var uploaded []byte
				for _, chunk := range chunks {
					if chunk.Offset != uint64(len(uploaded)) || chunk.Size != uint64(len(chunk.Data)) {
						t.Fatalf("incorrect chunk position: %+v", chunk)
					}
					uploaded = append(uploaded, chunk.Data...)
					var wantEvents []json.RawMessage
					if enabled {
						for i, report := range reports {
							start := bytes.Index(data, []byte(report))
							if start < 0 {
								t.Fatalf("report missing from stored log: %q", report)
							}
							end := uint64(start + len(report))
							if chunk.Offset < end && end <= chunk.Offset+chunk.Size {
								wantEvents = append(wantEvents, json.RawMessage(fmt.Sprintf(
									`{"log_byte_offset":%d,"log_byte_end":%d,"record":%s}`, start, end, records[i])))
							}
						}
					}
					if len(wantEvents) == 0 {
						if len(chunk.ExecutionEvents) != 0 {
							t.Errorf("unexpected sidecar on chunk %d: %s", chunk.Sequence, chunk.ExecutionEvents)
						}
						continue
					}
					want, err := json.Marshal(map[string]any{"version": 1, "events": wantEvents})
					if err != nil {
						t.Fatal(err)
					}
					var gotJSON, wantJSON any
					if err := json.Unmarshal(chunk.ExecutionEvents, &gotJSON); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(want, &wantJSON); err != nil {
						t.Fatal(err)
					}
					if diff := cmp.Diff(wantJSON, gotJSON); diff != "" {
						t.Errorf("chunk %d sidecar (-want +got):\n%s", chunk.Sequence, diff)
					}
				}
				if !bytes.Equal(uploaded, data) {
					t.Errorf("uploaded log differs from final bytes")
				}
			})
		}
	}
}

func TestLogStreamerProgramStatusLimits(t *testing.T) {
	t.Parallel()
	ctx, _ := experiments.Enable(t.Context(), experiments.ProgramStatus)
	// Use the longest legal ID and app, and all optional v1 fields. Even the
	// maximum 128-event sidecar fits within 64 KiB at these field limits.
	id := strings.Repeat("a", 32) + "/" + strings.Repeat("b", 32) + "/" + strings.Repeat("c", 32) + "/" + strings.Repeat("d", 29)
	report := "\x1b]7501;state=blocked:id=" + id + ":app=" + strings.Repeat("x", 32) + ":kind=permission:progress=100\a"
	// The tail starts a report after exceeding the event cap. It must still
	// complete in the next chunk, despite the preceding dropped reports.
	first := strings.Repeat(report, 129) + "\x1b]7501;state="
	second := "done:id=tail\a\x1b]7501;?\a\x1b]7501;state=invalid\a" +
		"\x1b]7501;state=done:unknown=" + strings.Repeat("x", 4096) + "\a" +
		"\x1b]7501;state=idle\a\x1b]7501;state=working" // incomplete at EOF
	var chunks []*api.Chunk
	ls := NewLogStreamer(logger.Discard, func(_ context.Context, chunk *api.Chunk) error {
		chunks = append(chunks, chunk)
		return nil
	}, LogStreamerConfig{Concurrency: 1, MaxChunkSizeBytes: uint64(len(first))})
	if err := ls.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{first, second} {
		if err := ls.Process(ctx, []byte(input)); err != nil {
			t.Fatal(err)
		}
	}
	ls.Stop()
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	for i, chunk := range chunks {
		var envelope struct {
			Version int `json:"version"`
			Events  []struct {
				Start  uint64         `json:"log_byte_offset"`
				End    uint64         `json:"log_byte_end"`
				Record map[string]any `json:"record"`
			} `json:"events"`
		}
		if err := json.Unmarshal(chunk.ExecutionEvents, &envelope); err != nil {
			t.Fatal(err)
		}
		wantCount := 128
		if i == 1 {
			wantCount = 2
		}
		if envelope.Version != 1 || len(envelope.Events) != wantCount || len(chunk.ExecutionEvents) > 65536 {
			t.Fatalf("chunk %d: version=%d events=%d bytes=%d", i, envelope.Version, len(envelope.Events), len(chunk.ExecutionEvents))
		}
		for j, event := range envelope.Events {
			if event.Start >= event.End || event.End-event.Start > 4096 || event.End <= chunk.Offset || event.End > chunk.Offset+chunk.Size {
				t.Errorf("invalid event offsets: %+v", event)
			}
			if i == 0 && (event.Start != uint64(j*len(report)) || event.End != uint64((j+1)*len(report))) {
				t.Errorf("event %d: offsets=[%d,%d)", j, event.Start, event.End)
			}
		}
		if i == 1 && (envelope.Events[0].Record["id"] != "tail" || envelope.Events[1].Record["state"] != "idle") {
			t.Errorf("unexpected events after limit: %+v", envelope.Events)
		}
	}
	if string(chunks[0].Data)+string(chunks[1].Data) != first+second {
		t.Error("event limits changed log bytes")
	}
}
