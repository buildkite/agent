package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/status"
	"github.com/dustin/go-humanize"
)

const defaultLogMaxSize = 1024 * 1024 * 1024 // 1 GiB

// Returned from Process after Stop has been called.
var errStreamerStopped = errors.New("streamer was already stopped")

// LogStreamerConfig contains configuration options for the log streamer.
type LogStreamerConfig struct {
	// How many log streamer workers are running at any one time
	Concurrency int

	// The maximum size of each chunk
	MaxChunkSizeBytes uint64

	// MaxChunkAge controls how frequently accumulated log data is flushed to
	// the upload queue. When 0, data is flushed on every Process call
	// (original behaviour). When >0, data accumulates between flushes,
	// reducing the number of API calls at the cost of a small latency increase.
	// The first flush always happens within 1s to keep the UI responsive.
	MaxChunkAge time.Duration

	// The maximum size of the log
	MaxSizeBytes uint64
}

// LogStreamer divides job log output into chunks (Process), and log streamer
// workers (goroutines created by Start) receive and upload those chunks.
// The actual uploading is performed by the callback.
type LogStreamer struct {
	// The configuration
	conf LogStreamerConfig

	// The logger instance to use
	logger logger.Logger

	// A counter of how many chunks failed to upload
	chunksFailedCount int32

	// The callback called when a chunk is ready for upload
	callback func(context.Context, *api.Chunk) error

	// The queue of chunks that are needing to be uploaded
	queue chan *api.Chunk

	// Total size in bytes of the log
	bytes uint64

	// Each chunk is assigned an order
	order uint64

	// Counts workers that are still running
	workerWG sync.WaitGroup

	// Only allow processing one at a time
	processMutex sync.Mutex

	// Have we logged a warning about the size?
	warnedAboutSize bool

	// Have we stopped?
	stopped bool

	// pending holds data not yet enqueued as a chunk.
	// Guarded by processMutex.
	pending []byte

	// stopCh is closed by Stop to signal the age flush goroutine to exit.
	stopCh chan struct{}
}

// NewLogStreamer creates a new instance of the log streamer.
func NewLogStreamer(
	agentLogger logger.Logger,
	callback func(context.Context, *api.Chunk) error,
	conf LogStreamerConfig,
) *LogStreamer {
	return &LogStreamer{
		logger:   agentLogger,
		conf:     conf,
		callback: callback,
		queue:    make(chan *api.Chunk, 1024),
		stopCh:   make(chan struct{}),
	}
}

// Start spins up a number of log streamer workers.
func (ls *LogStreamer) Start(ctx context.Context) error {
	if ls.conf.MaxChunkSizeBytes <= 0 {
		return errors.New("Maximum chunk size must be more than 0. No logs will be sent.")
	}

	if ls.conf.MaxSizeBytes <= 0 {
		ls.conf.MaxSizeBytes = defaultLogMaxSize
	}

	for i := range ls.conf.Concurrency {
		ls.workerWG.Go(func() { ls.worker(ctx, i) })
	}

	if ls.conf.MaxChunkAge > 0 {
		ls.workerWG.Go(func() { ls.ageFlushWorker(ctx) })
	}

	return nil
}

func (ls *LogStreamer) FailedChunks() int {
	return int(atomic.LoadInt32(&ls.chunksFailedCount))
}

// Process streams the output. It returns an error if the output data cannot be
// processed at all (e.g. the streamer was stopped or a hard limit was reached).
// Transient failures to upload logs are instead handled in the callback.
func (ls *LogStreamer) Process(ctx context.Context, output []byte) error {
	// Only allow one streamer process at a time
	ls.processMutex.Lock()
	defer ls.processMutex.Unlock()

	if ls.stopped {
		return errStreamerStopped
	}

	// Have we exceeded the max size?
	// (This check is also performed on the server side.)
	if ls.bytes+uint64(len(ls.pending))+uint64(len(output)) > ls.conf.MaxSizeBytes && !ls.warnedAboutSize {
		ls.logger.Warn("The job log has reached %s in size, which has "+
			"exceeded the maximum size (%s). Further logs may be dropped "+
			"by the server, and a future version of the agent will stop "+
			"sending logs at this point.",
			humanize.IBytes(ls.bytes), humanize.IBytes(ls.conf.MaxSizeBytes))
		ls.warnedAboutSize = true
		// In a future version, this will error out, e.g.:
		// return fmt.Errorf("%w (%d > %d)", errLogExceededMaxSize, ls.bytes, ls.conf.MaxSizeBytes)
	}

	ls.pending = append(ls.pending, output...)

	// When age-based flushing is active the ageFlushWorker owns the upload
	// cadence; just accumulate here. Without it, flush immediately (original
	// behaviour).
	if ls.conf.MaxChunkAge > 0 {
		return nil
	}
	return ls.flushAllPending(ctx)
}

// Stop stops the streamer.
func (ls *LogStreamer) Stop() {
	ls.processMutex.Lock()
	if ls.stopped {
		ls.processMutex.Unlock()
		return
	}
	ls.stopped = true
	close(ls.stopCh)
	// Flush any pending data before closing the queue. Use a background
	// context since the job context may already be cancelled at this point.
	_ = ls.flushAllPending(context.Background())
	close(ls.queue)
	ls.processMutex.Unlock()

	ls.logger.Debug("[LogStreamer] Waiting for workers to shut down")
	ls.workerWG.Wait()
}

// flushAllPending enqueues all pending data as chunks. Must be called with
// processMutex held.
func (ls *LogStreamer) flushAllPending(ctx context.Context) error {
	for len(ls.pending) > 0 {
		if err := ls.enqueueNextChunk(ctx); err != nil {
			return err
		}
	}
	return nil
}

// enqueueNextChunk takes up to MaxChunkSizeBytes from the front of pending
// and sends it to the upload queue. Must be called with processMutex held.
func (ls *LogStreamer) enqueueNextChunk(ctx context.Context) error {
	size := ls.conf.MaxChunkSizeBytes
	if pending := uint64(len(ls.pending)); size > pending {
		size = pending
	}

	data := make([]byte, size)
	copy(data, ls.pending[:size])
	ls.pending = ls.pending[size:]
	if len(ls.pending) == 0 {
		ls.pending = nil // release backing array
	}

	ls.order++
	chunk := &api.Chunk{
		Data:     data,
		Sequence: ls.order,
		Offset:   ls.bytes,
		Size:     size,
	}

	select {
	case ls.queue <- chunk:
	case <-ctx.Done():
		return ctx.Err()
	}
	ls.bytes += size
	return nil
}

// ageFlushWorker flushes pending data on a timer. The first flush uses a short
// 1s delay so the UI is populated quickly, then switches to MaxChunkAge for
// all subsequent flushes.
func (ls *LogStreamer) ageFlushWorker(ctx context.Context) {
	flush := func() {
		ls.processMutex.Lock()
		if !ls.stopped {
			_ = ls.flushAllPending(ctx)
		}
		ls.processMutex.Unlock()
	}

	// First flush: upload immediately so the job log has visible output within 1s of starting.
	initialDelay := min(time.Second, ls.conf.MaxChunkAge)
	select {
	case <-time.After(initialDelay):
		flush()
	case <-ls.stopCh:
		return
	case <-ctx.Done():
		return
	}

	// Subsequent flushes at the configured interval.
	ticker := time.NewTicker(ls.conf.MaxChunkAge)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			flush()
		case <-ls.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// The actual log streamer worker
func (ls *LogStreamer) worker(ctx context.Context, id int) {
	ls.logger.Debug("[LogStreamer/Worker#%d] Worker is starting...", id)

	defer ls.logger.Debug("[LogStreamer/Worker#%d] Worker has shutdown", id)

	ctx, setStat, done := status.AddSimpleItem(ctx, fmt.Sprintf("Log Streamer Worker %d", id))
	defer done()
	setStat("🏃 Starting...")

	for {
		setStat("⌚️ Waiting for a chunk")

		// Get the next chunk (pointer) from the queue. This will block
		// until something is returned.
		var chunk *api.Chunk
		select {
		case chunk = <-ls.queue:
			if chunk == nil { // channel was closed
				return
			}
		case <-ctx.Done(): // pack it up
			return
		}

		setStat("📨 Uploading chunk")

		// Upload the chunk
		err := ls.callback(ctx, chunk)
		if err != nil {
			atomic.AddInt32(&ls.chunksFailedCount, 1)

			ls.logger.Error("Giving up on uploading chunk %d, this will result in only a partial build log on Buildkite", chunk.Sequence)
		}
	}
}
