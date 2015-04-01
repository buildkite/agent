package logstreamer

import (
	"github.com/buildkite/agent/buildkite/logger"
	"math"
	"sync"
)

type Streamer struct {
	// The client we should use to stream the logs
	// Client Client

	// How many log streamer workers are running at any one time
	Concurrency int

	// The streaming work queue
	// Queue *work.Work
	//pool interface{}

	queue chan *Chunk

	// Total size in bytes of the log
	bytes int

	// The chunks of the log
	chunks []*Chunk

	// Each chunk is assigned an order
	order int

	// Every time we add a job to the queue, we increase the wait group
	// queue so when the streamer shuts down, we can block until all work
	// has been added.
	chunkWaitGroup sync.WaitGroup
}

func New() (*Streamer, error) {
	// Create a new log streamer and default the concurrency to 5, seems
	// like a good number?
	streamer := new(Streamer)
	streamer.Concurrency = 5
	streamer.queue = make(chan *Chunk, 1024)

	return streamer, nil
}

func (streamer *Streamer) Start() error {
	// spawn workers
	for i := 0; i < streamer.Concurrency; i++ {
		go Worker(i, streamer)
	}

	return nil
}

func Worker(id int, streamer *Streamer) {
	logger.Debug("Streamer worker %d is running", id)

	var chunk *Chunk
	for {
		// Get the next chunk (pointer) from the queue. This will block
		// until something is returned.
		chunk = <-streamer.queue

		// If the next chunk is nil, then there is no more work to do
		if chunk == nil {
			break
		}

		chunk.Upload()
	}

	streamer.chunkWaitGroup.Done()

	logger.Debug("Streamer worker %d has shut down", id)
}

// Takes the full process output, grabs the portion we don't have, and adds it
// to the stream queue
func (streamer *Streamer) Process(output string) error {
	bytes := len(output)

	if streamer.bytes != bytes {
		maximumBlobSize := 100000

		// Grab the part of the log that we haven't seen yet
		blob := output[streamer.bytes:bytes]

		// How many 100kb chunks are in the blob?
		numberOfChunks := int(math.Ceil(float64(len(blob)) / float64(maximumBlobSize)))

		// Increase the wait group by the amount of chunks we're going
		// to add
		streamer.chunkWaitGroup.Add(numberOfChunks)

		for i := 0; i < numberOfChunks; i++ {
			// Find the upper limit of the blob
			upperLimit := (i + 1) * maximumBlobSize
			if upperLimit > len(blob) {
				upperLimit = len(blob)
			}

			// Grab the 100kb section of the blob
			partialBlob := blob[i*maximumBlobSize : upperLimit]

			// Increment the order
			streamer.order += 1

			// logger.Debug("Creating %d byte chunk and adding work %d", len(partialBlob), streamer.order)

			// Create the chunk and append it to our list
			chunk := Chunk{
				Order: streamer.order,
				Blob:  partialBlob,
				Bytes: len(partialBlob),
			}

			// Append the chunk to our list
			streamer.chunks = append(streamer.chunks, &chunk)

			// Create the worker and run it
			//worker := Worker{Chunk: &chunk}
			//go func() {
			// Add the chunk worker to the queue. Run will
			// block until it is successfully added to the
			// queue.
			//streamer.Queue.Run(&worker)
			// streamer.pool.SendWork(chunk)
			streamer.queue <- &chunk

			// Tell the wait group that this job has been
			// added to the queue
			//streamer.chunkWaitGroup.Done()
			//}()
		}

		// Save the new amount of bytes
		streamer.bytes = bytes
	}

	return nil
}

func (streamer *Streamer) Stop() error {
	logger.Debug("Log streamer is waiting for all the chunks to be added to the queue")
	streamer.chunkWaitGroup.Wait()

	logger.Debug("Log streamer is waiting for all the chunks to be uploaded")
	// all work is done
	// signal workers there is no more work
	for n := 0; n < streamer.Concurrency; n++ {
		streamer.queue <- nil
	}

	return nil
}
