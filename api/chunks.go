package api

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"

	"github.com/klauspost/compress/gzip"
)

// Chunk represents a Buildkite Agent API Chunk
type Chunk struct {
	Data            []byte
	Sequence        uint64
	Offset          uint64
	Size            uint64
	CompressedBytes uint64
	ExecutionEvents []byte // Optional versioned program-status sidecar JSON.
}

// Uploads the chunk to the Buildkite Agent API. This request sends the
// compressed log directly as a request body, or as a multipart form when
// program-status events accompany it.
func (c *Client) UploadChunk(ctx context.Context, jobId string, chunk *Chunk) (*Response, error) {
	// Create a compressed buffer of the log content
	body := &bytes.Buffer{}
	gzipper := gzip.NewWriter(body)
	if _, err := gzipper.Write(chunk.Data); err != nil {
		return nil, err
	}
	if err := gzipper.Close(); err != nil {
		return nil, err
	}
	chunk.CompressedBytes = uint64(body.Len())

	contentType, contentEncoding := "text/plain", "gzip"
	if len(chunk.ExecutionEvents) > 0 {
		// If optional metadata cannot be encoded, send the log alone.
		if form, formType, err := chunkForm(body.Bytes(), chunk.ExecutionEvents); err == nil {
			body, contentType, contentEncoding = form, formType, ""
		}
	}

	// Pass most params as query
	u := fmt.Sprintf("jobs/%s/chunks?sequence=%d&offset=%d&size=%d", railsPathEscape(jobId), chunk.Sequence, chunk.Offset, chunk.Size)
	req, err := c.newFormRequest(ctx, "POST", u, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", contentType)
	if contentEncoding != "" {
		req.Header.Set("Content-Encoding", contentEncoding)
	}

	return c.doRequest(req, nil)
}

func chunkForm(compressed, events []byte) (*bytes.Buffer, string, error) {
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	if err := form.WriteField("execution_events", string(events)); err != nil {
		return nil, "", err
	}
	part, err := form.CreateFormFile("chunk", "chunk.gz")
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(compressed); err != nil {
		return nil, "", err
	}
	if err := form.Close(); err != nil {
		return nil, "", err
	}
	return body, form.FormDataContentType(), nil
}
