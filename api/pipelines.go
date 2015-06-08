package api

import (
	"bytes"
	"fmt"
	"mime"
	"mime/multipart"
	"path/filepath"
)

// PipelinesService handles communication with the pipeline related methods of the
// Buildkite Agent API.
type PipelinesService struct {
	client *Client
}

// Pipeline represents a Buildkite Agent API Pipeline
type Pipeline struct {
	Data     []byte
	FileName string
}

// Uploads the pipeline to the Buildkite Agent API. This request doesn't use JSON,
// but a multi-part HTTP form upload
func (cs *PipelinesService) Upload(jobId string, pipeline *Pipeline) (*Response, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Default the filename
	fileName := pipeline.FileName
	if fileName == "" {
		fileName = "pipeline"
	}

	// Calculate the mime type based on the filename
	extension := filepath.Ext(fileName)
	contentType := mime.TypeByExtension(extension)
	if contentType == "" {
		contentType = "binary/octet-stream"
	}

	// Write the pipeline to the form
	part, _ := createFormFileWithContentType(writer, "pipeline", fileName, contentType)
	part.Write([]byte(pipeline.Data))

	// Close the writer because we don't need to add any more values to it
	err := writer.Close()
	if err != nil {
		return nil, err
	}

	u := fmt.Sprintf("v2/jobs/%s/pipelines", jobId)
	req, err := cs.client.NewFormRequest("POST", u, body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", writer.FormDataContentType())

	return cs.client.Do(req, nil)
}
