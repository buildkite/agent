package buildkite

import (
	"github.com/buildkite/agent/buildkite/logger"
)

type LogStreamerWorker struct {
	// We assign an ID to the worker so we know which one is which
	ID int

	// The current chunk the worker is uploading
	LogStreamerChunk chan LogStreamerChunk

	// The queue is should be getting work from
	LogStreamerWorkerQueue chan chan LogStreamerChunk

	// The channel listen to for stop events
	QuitChan chan bool
}

func NewLogStreamerWorker(id int, logStreamerWorkerQueue chan chan LogStreamerChunk) LogStreamerWorker {
	worker := LogStreamerWorker{
		ID:                     id,
		LogStreamerChunk:       make(chan LogStreamerChunk),
		LogStreamerWorkerQueue: logStreamerWorkerQueue,
		QuitChan:               make(chan bool),
	}

	return worker
}

func (worker *LogStreamerWorker) Start() {
	go func() {
		for {
			// Add ourselves into the worker queue.
			worker.LogStreamerWorkerQueue <- worker.LogStreamerChunk

			select {
			case work := <-worker.LogStreamerChunk:
				// Receive a work request.
				logger.Debug("worker%d: Received work request: %s", worker.ID, work)

			case <-worker.QuitChan:
				// We have been asked to stop.
				logger.Debug("worker%d stopping\n", worker.ID)
				return
			}
		}
	}()
}

// Stop tells the worker to stop listening for work requests. Note: that the
// worker will only stop *after* it has finished its work.
func (worker *LogStreamerWorker) Stop() {
	go func() {
		worker.QuitChan <- true
	}()
}
