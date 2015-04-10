package logstreamer

import (
	"github.com/buildkite/agent/buildkite/http"
	"github.com/buildkite/agent/buildkite/logger"
)

type Chunk struct {
	// The HTTP request we'll send logs to
	Request *http.Request

	// The contents of the chunk
	Data string

	// The sequence number of this chunk
	Order int
}

func (chunk *Chunk) Upload() {
	// Take a copy of the request and make it our own
	r := chunk.Request.Copy()

	// Add the chunk to the request as a multipart form upload
	r.Params["chunk"] = http.MultiPart{
		Data:     chunk.Data,
		MimeType: "text/plain",
		FileName: "chunk.txt",
	}

	// Set the order as another parameter
	r.Params["order"] = chunk.Order

	// Perform the chunk upload
	_, err := r.Do()
	if err != nil {
		logger.Error("Giving up on uploading chunk %d, this will result in only a partial build log on Buildkite", chunk.Order)
	} else {
		logger.Debug("Uploaded chunk %d", chunk.Order)
	}
}
