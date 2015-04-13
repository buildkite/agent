package logstreamer

import (
	"github.com/buildkite/agent/buildkite/http"
	"github.com/buildkite/agent/buildkite/logger"
	"math"
	"sync"
	"sync/atomic"
)

type Streamer struct {
	// How many log streamer workers are running at any one time
	Concurrency int

	// The base HTTP request we'll keep sending logs to
	Request *http.Request

	// The maximum size of chunks
	MaxChunkSizeBytes int

	// A counter of how many chunks failed to upload
	FailedChunksCount int32

	// The queue of chunks that are needing to be uploaded
	queue chan *Chunk

	// Total size in bytes of the log
	bytes int

	// Each chunk is assigned an order
	order int

	// Every time we add a job to the queue, we increase the wait group
	// queue so when the streamer shuts down, we can block until all work
	// has been added.
	chunkWaitGroup sync.WaitGroup

	// Only allow processing one at a time
	processMutex sync.Mutex
}

// Creates a new instance of the log streamer
func New(request http.Request, maxChunkSizeBytes int) (*Streamer, error) {
	// Create a new log streamer and default the concurrency
	streamer := new(Streamer)
	streamer.Concurrency = 3
	streamer.Request = &request
	streamer.MaxChunkSizeBytes = maxChunkSizeBytes
	streamer.queue = make(chan *Chunk, 1024)

	return streamer, nil
}

// Spins up x number of log streamer workers
func (streamer *Streamer) Start() error {
	for i := 0; i < streamer.Concurrency; i++ {
		go Worker(i, streamer)
	}

	return nil
}

// Takes the full process output, grabs the portion we don't have, and adds it
// to the stream queue
func (streamer *Streamer) Process(output string) error {
	bytes := len(output)

	// Only allow one streamer process at a time
	streamer.processMutex.Lock()

	if streamer.bytes != bytes {
		// Grab the part of the log that we haven't seen yet
		blob := output[streamer.bytes:bytes]

		// How many chunks do we have that fit within the MaxChunkSizeBytes?
		numberOfChunks := int(math.Ceil(float64(len(blob)) / float64(streamer.MaxChunkSizeBytes)))

		// Increase the wait group by the amount of chunks we're going
		// to add
		streamer.chunkWaitGroup.Add(numberOfChunks)

		for i := 0; i < numberOfChunks; i++ {
			// Find the upper limit of the blob
			upperLimit := (i + 1) * streamer.MaxChunkSizeBytes
			if upperLimit > len(blob) {
				upperLimit = len(blob)
			}

			// Grab the 100kb section of the blob
			partialChunk := blob[i*streamer.MaxChunkSizeBytes : upperLimit]

			// Increment the order
			streamer.order += 1

			// Create the chunk and append it to our list
			chunk := Chunk{
				Request: streamer.Request,
				Data:    partialChunk,
				Order:   streamer.order,
			}

			streamer.queue <- &chunk
		}

		// Save the new amount of bytes
		streamer.bytes = bytes
	}

	streamer.processMutex.Unlock()

	return nil
}

// Waits for all the chunks to be uploaded, then shuts down all the workers
func (streamer *Streamer) Stop() error {
	logger.Debug("[LogStreamer] Waiting for all the chunks to be uploaded")
	streamer.chunkWaitGroup.Wait()

	logger.Debug("[LogStreamer] Shutting down all workers")
	for n := 0; n < streamer.Concurrency; n++ {
		streamer.queue <- nil
	}

	return nil
}

// The actual log streamer worker
func Worker(id int, streamer *Streamer) {
	logger.Debug("[LogStreamer/Worker#%d] Worker is starting...", id)

	var chunk *Chunk
	for {
		// Get the next chunk (pointer) from the queue. This will block
		// until something is returned.
		chunk = <-streamer.queue

		// If the next chunk is nil, then there is no more work to do
		if chunk == nil {
			break
		}

		// Upload the chunk
		err := chunk.Upload()
		if err != nil {
			atomic.AddInt32(&streamer.FailedChunksCount, 1)

			logger.Error("Giving up on uploading chunk %d, this will result in only a partial build log on Buildkite", chunk.Order)
		}

		// Signal to the chunkWaitGroup that this one is done
		streamer.chunkWaitGroup.Done()
	}

	logger.Debug("[LogStreamer/Worker#%d] Worker has shutdown", id)
}
