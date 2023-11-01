package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/status"
	"github.com/dustin/go-humanize"
)

const defaultLogMaxSize = 1024 * 1024 * 1024 // 1 GiB

type LogStreamerConfig struct {
	// How many log streamer workers are running at any one time
	Concurrency int

	// The maximum size of each chunk
	MaxChunkSizeBytes uint64

	// The maximum size of the log
	MaxSizeBytes uint64
}

type LogStreamer struct {
	// The configuration
	conf LogStreamerConfig

	// The logger instance to use
	logger logger.Logger

	// A counter of how many chunks failed to upload
	chunksFailedCount int32

	// The callback called when a chunk is ready for upload
	callback func(context.Context, *LogStreamerChunk) error

	// The queue of chunks that are needing to be uploaded
	queue chan *LogStreamerChunk

	// Total size in bytes of the log
	bytes uint64

	// Each chunk is assigned an order
	order uint64

	// Every time we add a job to the queue, we increase the wait group
	// queue so when the streamer shuts down, we can block until all work
	// has been added.
	chunkWaitGroup sync.WaitGroup

	// Only allow processing one at a time
	processMutex sync.Mutex
}

type LogStreamerChunk struct {
	// The contents of the chunk
	Data []byte

	// The sequence number of this chunk
	Order uint64

	// The byte offset of this chunk
	Offset uint64

	// The byte size of this chunk
	Size uint64
}

// Creates a new instance of the log streamer
func NewLogStreamer(l logger.Logger, cb func(context.Context, *LogStreamerChunk) error, c LogStreamerConfig) *LogStreamer {
	return &LogStreamer{
		logger:   l,
		conf:     c,
		callback: cb,
		queue:    make(chan *LogStreamerChunk, 1024),
	}
}

// Spins up x number of log streamer workers
func (ls *LogStreamer) Start(ctx context.Context) error {
	if ls.conf.MaxChunkSizeBytes <= 0 {
		return errors.New("Maximum chunk size must be more than 0. No logs will be sent.")
	}

	if ls.conf.MaxSizeBytes <= 0 {
		ls.conf.MaxSizeBytes = defaultLogMaxSize
	}

	for i := 0; i < ls.conf.Concurrency; i++ {
		go ls.worker(ctx, i)
	}

	return nil
}

func (ls *LogStreamer) FailedChunks() int {
	return int(atomic.LoadInt32(&ls.chunksFailedCount))
}

// Process streams the output.
func (ls *LogStreamer) Process(output []byte) {
	// Only allow one streamer process at a time
	ls.processMutex.Lock()
	defer ls.processMutex.Unlock()

	warnedAboutSize := false

	for len(output) > 0 {
		// Have we exceeded the max size?
		// (This check is also performed on the server side.)
		if ls.bytes > ls.conf.MaxSizeBytes && !warnedAboutSize {
			ls.logger.Warn("The job log has reached %s in size, which has "+
				"exceeded the maximum size (%s). Further logs may be dropped "+
				"by the server, and a future version of the agent will stop "+
				"sending logs at this point.",
				humanize.Bytes(ls.bytes), humanize.Bytes(ls.conf.MaxSizeBytes))
			warnedAboutSize = true
			// In a future version, this will error out, e.g.:
			//return fmt.Errorf("job log has exceeded max job log size (%d > %d)", ls.bytes, ls.conf.MaxSizeBytes)
		}

		// Add another chunk...
		ls.chunkWaitGroup.Add(1)

		// Find the upper limit of the blob
		size := ls.conf.MaxChunkSizeBytes
		if lenout := uint64(len(output)); size > lenout {
			size = lenout
		}

		// Grab the ≤100kb section of the blob.
		// Leave the remainder for the next iteration.
		chunk := output[:size]
		output = output[size:]

		ls.order++

		// Create the chunk and append it to our list
		ls.queue <- &LogStreamerChunk{
			Data:   chunk,
			Order:  ls.order,
			Offset: ls.bytes,
			Size:   size,
		}

		// Save the new amount of bytes
		ls.bytes += size
	}
}

// Waits for all the chunks to be uploaded, then shuts down all the workers
func (ls *LogStreamer) Stop() error {
	ls.logger.Debug("[LogStreamer] Waiting for all the chunks to be uploaded")

	ls.chunkWaitGroup.Wait()

	ls.logger.Debug("[LogStreamer] Shutting down all workers")

	for n := 0; n < ls.conf.Concurrency; n++ {
		ls.queue <- nil
	}

	return nil
}

// The actual log streamer worker
func (ls *LogStreamer) worker(ctx context.Context, id int) {
	ls.logger.Debug("[LogStreamer/Worker#%d] Worker is starting...", id)

	ctx, setStat, done := status.AddSimpleItem(ctx, fmt.Sprintf("Log Streamer Worker %d", id))
	defer done()
	setStat("🏃 Starting...")

	for {
		setStat("⌚️ Waiting for chunk")

		// Get the next chunk (pointer) from the queue. This will block
		// until something is returned.
		chunk := <-ls.queue

		// If the next chunk is nil, then there is no more work to do
		if chunk == nil {
			break
		}

		setStat("📨 Passing chunk to callback")

		// Upload the chunk
		err := ls.callback(ctx, chunk)
		if err != nil {
			atomic.AddInt32(&ls.chunksFailedCount, 1)

			ls.logger.Error("Giving up on uploading chunk %d, this will result in only a partial build log on Buildkite", chunk.Order)
		}

		// Signal to the chunkWaitGroup that this one is done
		ls.chunkWaitGroup.Done()
	}

	ls.logger.Debug("[LogStreamer/Worker#%d] Worker has shutdown", id)
}
