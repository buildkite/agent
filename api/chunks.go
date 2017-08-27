package api

import (
	"bytes"
	"compress/gzip"
	"fmt"
)

// ChunksService handles communication with the chunk related methods of the
// Buildkite Agent API.
type ChunksService struct {
	client *Client
}

// Chunk represents a Buildkite Agent API Chunk
type Chunk struct {
	Data     string
	Sequence int
	Offset   int
	Size     int
}

// Uploads the chunk to the Buildkite Agent API. This request sends the
// compressed log directly as a request body.
func (cs *ChunksService) Upload(jobId string, chunk *Chunk) (*Response, error) {
	// Create a compressed buffer of the log content
	body := &bytes.Buffer{}
	gzipper := gzip.NewWriter(body)
	gzipper.Write([]byte(chunk.Data))
	if err := gzipper.Close(); err != nil {
		return nil, err
	}

	// Pass most params as query
	u := fmt.Sprintf("jobs/%s/chunks?sequence=%d&offset=%d&size=%d", jobId, chunk.Sequence, chunk.Offset, chunk.Size)
	req, err := cs.client.NewFormRequest("POST", u, body)
	if err != nil {
		return nil, err
	}

	// Mark the request as a direct compressed log chunk
	req.Header.Add("Content-Type", "text/plain")
	req.Header.Add("Content-Encoding", "gzip")

	return cs.client.Do(req, nil)
}
