package buildkite

import (
	"github.com/buildkite/agent/buildkite/logger"
	"time"
)

type LogStreamerWorker struct {
	Chunk *LogStreamerChunk
}

func (worker *LogStreamerWorker) Work(id int) {
	logger.Debug("Uploading %d bytes of content at order %d", worker.Chunk.Bytes, worker.Chunk.Order)
	time.Sleep(time.Second * 5)
	logger.Debug("Finished %d", worker.Chunk.Order)
}
