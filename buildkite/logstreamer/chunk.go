package logstreamer

import (
	"github.com/buildkite/agent/buildkite/http"
)

type Chunk struct {
	// The HTTP request we'll send logs to
	Request *http.Request

	// The contents of the chunk
	Data string

	// The sequence number of this chunk
	Order int
}

func (chunk *Chunk) Upload() error {
	// Take a copy of the request and make it our own
	r := chunk.Request.Copy()

	// Add the chunk to the request as a multipart form upload
	r.Params["blob"] = http.MultiPart{
		Data:     chunk.Data,
		MimeType: "text/plain",
		FileName: "chunk.txt",
	}

	// Set the order as another parameter
	r.Params["sequence"] = chunk.Order

	// Perform the chunk upload
	_, err := r.Do()
	if err != nil {
		return err
	} else {
		return nil
	}
}
