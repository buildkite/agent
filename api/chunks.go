package api

import (
	"bytes"
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
}

// Uploads the chunk to the Buildkite Agent API. This request doesn't use JSON,
// but a multi-part HTTP form upload
func (cs *ChunksService) Upload(jobId string, chunk *Chunk) (*Response, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Write the chunk to the form
	part, _ := writer.CreateFormFile("chunk", "chunk.txt")
	part.Write([]byte(chunk.Data))

	// Write the sequence value to the form
	writer.WriteField("sequence", fmt.Sprintf("%d", chunk.Sequence))

	// Close the writer because we don't need to add any more values to it
	err := writer.Close()
	if err != nil {
		return nil, err
	}

	u := fmt.Sprintf("v2/jobs/%s/chunks", jobId)
	req, err := cs.client.NewFormRequest("POST", u, body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", writer.FormDataContentType())

	return cs.client.Do(req, nil)
}
