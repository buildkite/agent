package api

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"mime/multipart"
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

// Uploads the chunk to the Buildkite Agent API. This request doesn't use JSON,
// but a multi-part HTTP form upload
func (cs *ChunksService) Upload(jobId string, chunk *Chunk) (*Response, error) {
	body := &bytes.Buffer{}
	gzipper := gzip.NewWriter(body)
	writer := multipart.NewWriter(gzipper)

	// Write the sequence, offset and size values to the form
	writer.WriteField("sequence", fmt.Sprintf("%d", chunk.Sequence))
	writer.WriteField("offset", fmt.Sprintf("%d", chunk.Offset))
	writer.WriteField("size", fmt.Sprintf("%d", chunk.Size))

	// Write the chunk to the form
	part, _ := writer.CreateFormFile("chunk", "chunk")
	part.Write([]byte(chunk.Data))

	// Close the writer and gzipper to finalise the buffer
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if err := gzipper.Close(); err != nil {
		return nil, err
	}

	u := fmt.Sprintf("jobs/%s/chunks", jobId)
	req, err := cs.client.NewFormRequest("POST", u, body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", writer.FormDataContentType())
	req.Header.Add("Content-Encoding", "gzip")

	return cs.client.Do(req, nil)
}
