package api

import (
	"context"
	"fmt"
	"time"
)

// Artifact represents an artifact on the Buildkite Agent API
type Artifact struct {
	// The ID of the artifact. The ID is assigned to it after a successful
	// batch creation
	ID string `json:"id"`

	// The path to the artifact relative to the working directory
	Path string `json:"path"`

	// The absolute path to the artifact
	AbsolutePath string `json:"absolute_path"`

	// The glob path used to find this artifact
	GlobPath string `json:"glob_path"`

	// The size of the file in bytes
	FileSize int64 `json:"file_size"`

	// A SHA-1 hash of the uploaded file
	Sha1Sum string `json:"sha1sum"`

	// A SHA-2 256-bit hash of the uploaded file, possibly empty
	Sha256Sum string `json:"sha256sum"`

	// ID of the job that created this artifact (from API)
	JobID string `json:"job_id"`

	// UTC timestamp this artifact was considered created
	CreatedAt time.Time `json:"created_at"`

	// The HTTP url to this artifact once it's been uploaded
	URL string `json:"url,omitempty"`

	// The destination specified on the command line when this file was
	// uploaded
	UploadDestination string `json:"upload_destination,omitempty"`

	// Information on how to upload this artifact.
	UploadInstructions *ArtifactUploadInstructions `json:"-"`

	// A specific Content-Type to use on upload
	ContentType string `json:"content_type,omitempty"`
}

type ArtifactBatch struct {
	ID                string      `json:"id"`
	Artifacts         []*Artifact `json:"artifacts"`
	UploadDestination string      `json:"upload_destination"`
}

type ArtifactUploadInstructions struct {
	Data   map[string]string `json:"data"`
	Action struct {
		URL       string `json:"url,omitempty"`
		Method    string `json:"method"`
		Path      string `json:"path"`
		FileInput string `json:"file_input"`
	}
}

type ArtifactBatchCreateResponse struct {
	ID                 string                      `json:"id"`
	ArtifactIDs        []string                    `json:"artifact_ids"`
	UploadInstructions *ArtifactUploadInstructions `json:"upload_instructions"`
}

// ArtifactSearchOptions specifies the optional parameters to the
// ArtifactsService.Search method.
type ArtifactSearchOptions struct {
	Query              string `url:"query,omitempty"`
	Scope              string `url:"scope,omitempty"`
	State              string `url:"state,omitempty"`
	IncludeRetriedJobs bool   `url:"include_retried_jobs,omitempty"`
	IncludeDuplicates  bool   `url:"include_duplicates,omitempty"`
}

type ArtifactBatchUpdateArtifact struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

type ArtifactBatchUpdateRequest struct {
	Artifacts []*ArtifactBatchUpdateArtifact `json:"artifacts"`
}

// CreateArtifacts takes a slice of artifacts, and creates them on Buildkite as a batch.
func (c *Client) CreateArtifacts(ctx context.Context, jobId string, batch *ArtifactBatch) (*ArtifactBatchCreateResponse, *Response, error) {
	u := fmt.Sprintf("jobs/%s/artifacts", railsPathEscape(jobId))

	req, err := c.newRequest(ctx, "POST", u, batch)
	if err != nil {
		return nil, nil, err
	}

	createResponse := new(ArtifactBatchCreateResponse)
	resp, err := c.doRequest(req, createResponse)
	if err != nil {
		return nil, resp, err
	}

	return createResponse, resp, err
}

// Updates a particular artifact
func (c *Client) UpdateArtifacts(ctx context.Context, jobId string, artifactStates map[string]string) (*Response, error) {
	u := fmt.Sprintf("jobs/%s/artifacts", railsPathEscape(jobId))
	payload := ArtifactBatchUpdateRequest{}

	for id, state := range artifactStates {
		payload.Artifacts = append(payload.Artifacts, &ArtifactBatchUpdateArtifact{id, state})
	}

	req, err := c.newRequest(ctx, "PUT", u, payload)
	if err != nil {
		return nil, err
	}

	resp, err := c.doRequest(req, nil)
	if err != nil {
		return resp, err
	}

	return resp, err
}

// SearchArtifacts searches Buildkite for a set of artifacts
func (c *Client) SearchArtifacts(ctx context.Context, buildId string, opt *ArtifactSearchOptions) ([]*Artifact, *Response, error) {
	u := fmt.Sprintf("builds/%s/artifacts/search", railsPathEscape(buildId))
	u, err := addOptions(u, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := c.newRequest(ctx, "GET", u, nil)
	if err != nil {
		return nil, nil, err
	}

	a := []*Artifact{}
	resp, err := c.doRequest(req, &a)
	if err != nil {
		return nil, resp, err
	}

	return a, resp, err
}
