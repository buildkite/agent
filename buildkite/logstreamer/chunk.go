package logstreamer

import (
	"github.com/buildkite/agent/http"
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

	r.Body = &http.Form{
		Params: map[string]interface{}{
			"chunk": http.File{
				Data:     chunk.Data,
				MimeType: "text/plain",
				FileName: "chunk.txt",
			},
			"sequence": chunk.Order,
		},
	}

	// Perform the chunk upload
	_, err := r.Do()
	if err != nil {
		return err
	} else {
		return nil
	}
}
