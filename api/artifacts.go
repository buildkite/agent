package api

import (
	"fmt"
)

// ArtifactsService handles communication with the artifact related methods of
// the Buildkite Artifact API.
type ArtifactsService struct {
	client *Client
}

// Artifact represents an artifact on the Buildkite Agent API
type Artifact struct {
	// The ID of the artifact. The ID is assigned to it after a successful
	// batch creation
	ID string `json:"-"`

	// The path to the artifact relative to the working directory
	Path string `json:"path"`

	// The absolute path to the artifact
	AbsolutePath string `json:"absolute_path"`

	// The glob path used to find this artifact
	GlobPath string `json:"glob_path"`

	// The size of the file in bytes
	FileSize int64 `json:"file_size"`

	// A Sha1Sum calculation of the file
	Sha1Sum string `json:"sha1sum"`

	// The HTTP url to this artifact once it's been uploaded
	URL string `json:"url,omitempty"`

	// The destination specified on the command line when this file was
	// uploaded
	UploadDestination string `json:"upload_destination,omitempty"`

	// Information on how to upload this artifact.
	UploadInstructions *ArtifactUploadInstructions `json:"-"`
}

type ArtifactBatch struct {
	ID                string      `json:"id"`
	Artifacts         []*Artifact `json:"artifacts"`
	UploadDestination string      `json:"upload_destination"`
}

type ArtifactUploadInstructions struct {
	Data   map[string]string `json: "data"`
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
	Query string `url:"query,omitempty"`
	Scope string `url:"scope,omitempty"`
}

type ArtifactUpdateStateRequest struct {
	State string `json:"state"`
}

// Accepts a slice of artifacts, and creates them on Buildkite as a batch.
func (as *ArtifactsService) Create(jobId string, batch *ArtifactBatch) (*ArtifactBatchCreateResponse, *Response, error) {
	u := fmt.Sprintf("jobs/%s/artifacts", jobId)

	req, err := as.client.NewRequest("POST", u, batch)
	if err != nil {
		return nil, nil, err
	}

	createResponse := new(ArtifactBatchCreateResponse)
	resp, err := as.client.Do(req, createResponse)
	if err != nil {
		return nil, resp, err
	}

	return createResponse, resp, err
}

// Updates a paticular artifact
func (as *ArtifactsService) UpdateState(jobId string, artifactId string, state string) (*Response, error) {
	u := fmt.Sprintf("jobs/%s/artifacts/%s", jobId, artifactId)
	payload := ArtifactUpdateStateRequest{state}

	req, err := as.client.NewRequest("PUT", u, payload)
	if err != nil {
		return nil, err
	}

	resp, err := as.client.Do(req, nil)
	if err != nil {
		return resp, err
	}

	return resp, err
}

// Searches Buildkite for a set of artifacts
func (as *ArtifactsService) Search(buildId string, opt *ArtifactSearchOptions) ([]*Artifact, *Response, error) {
	u := fmt.Sprintf("builds/%s/artifacts/search", buildId)
	u, err := addOptions(u, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := as.client.NewRequest("GET", u, nil)
	if err != nil {
		return nil, nil, err
	}

	a := []*Artifact{}
	resp, err := as.client.Do(req, &a)
	if err != nil {
		return nil, resp, err
	}

	return a, resp, err
}
