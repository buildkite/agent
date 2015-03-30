package buildkite

import (
	"github.com/buildkite/agent/buildkite/logger"
	"github.com/goinggo/work"
)

type LogStreamer struct {
	// The client we should use to stream the logs
	client Client

	// How many log streamer workers are running at any one time
	Concurrency int

	// The queue
	// LogStreamerWorkerQueue chan chan LogStreamerChunk

	// Total size in bytes of the log
	bytes int

	// The chunks of the log
	chunks []LogStreamerChunk
}

func NewLogStreamer(client *Client) (*LogStreamer, error) {
	streamer := new(LogStreamer)
	streamer.Concurrency = 5
	// streamer.LogStreamerWorkerQueue = make(chan chan LogStreamerChunk, streamer.Concurrency)

	return streamer, nil
}

func (streamer *LogStreamer) Start() {
	// Create workers to process the logs
	for i := 0; i < streamer.Concurrency; i++ {
		logger.Debug("Starting log streamer worker %d/%d", i+1, streamer.Concurrency)

		worker := NewLogStreamerWorker(i+1, streamer.LogStreamerWorkerQueue)
		worker.Start()
	}
}

// Takes the full process output, grabs the portion we don't have, and adds it
// to the stream queue
func (streamer *LogStreamer) Process(output string) {
	// logger.Debug("Output: %s", output)

	// outputRunes := utf8string.NewString(output)
	// outputRuneCount := outputRunes.RuneCount()

	// if job.lastRuneCount != outputRuneCount {
	// 	logger.Debug("Output changed from %d to %d", job.lastRuneCount, outputRuneCount)

	// 	// Grab the difference between the two outputs
	// 	length := outputRuneCount - job.lastRuneCount
	// 	logger.Debug("Getting %d to %d of new output", outputRuneCount, length)

	// 	difference := outputRunes.Slice(job.lastRuneCount, outputRuneCount)
	// 	logger.Debug("part: %s", difference)

	// 	// Save the output to the job
	// 	//job.Output = output
	// 	job.lastRuneCount = outputRuneCount
	// }

	// return nil
}

// Waits until the streaming has finished
func (streamer *LogStreamer) Wait() error {
	logger.Debug("Waiting for the log streaming workers to finish")

	// for _, r := range routines {
	// 	<-r
	// }

	return nil
}
