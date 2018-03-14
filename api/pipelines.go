package api

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"path/filepath"

	"github.com/buildkite/agent/mime"
)

// PipelinesService handles communication with the pipeline related methods of the
// Buildkite Agent API.
type PipelinesService struct {
	client *Client
}

// Pipeline represents a Buildkite Agent API Pipeline
type Pipeline struct {
	UUID     string
	Data     []byte
	FileName string
	Replace  bool
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
	part, err := createFormFileWithContentType(writer, "pipeline", fileName, contentType)
	if err != nil {
		return nil, err
	}
	part.Write([]byte(pipeline.Data))

	// Add the replace option
	writer.WriteField("replace", fmt.Sprintf("%t", pipeline.Replace))

	// The pipeline upload endpoint requires a way for it to uniquely
	// identify this upload (because it's an idempotent endpoint). If a job
	// tries to upload a pipeline that matches a previously uploaded one
	// with a matching uuid, then it'll just return and not do anything.
	writer.WriteField("uuid", pipeline.UUID)

	// Close the writer because we don't need to add any more values to it
	err = writer.Close()
	if err != nil {
		return nil, err
	}

	u := fmt.Sprintf("jobs/%s/pipelines", jobId)
	req, err := cs.client.NewFormRequest("POST", u, body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", writer.FormDataContentType())

	return cs.client.Do(req, nil)
}
