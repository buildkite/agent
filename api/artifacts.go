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
	ID           string `json:"id,omitempty"`
	State        string `json:"state,omitempty"`
	Path         string `json:"path"`
	AbsolutePath string `json:"absolute_path"`
	GlobPath     string `json:"glob_path"`
	FileSize     int64  `json:"file_size"`
	Sha1Sum      string `json:"sha1sum"`
	URL          string `json:"url,omitempty"`
	Uploader     struct {
		Data   map[string]string `json: "data"`
		Action struct {
			URL       string `json:"url,omitempty"`
			Method    string `json:"method"`
			Path      string `json:"path"`
			FileInput string `json:"file_input"`
		}
	}
}

// ArtifactSearchOptions specifies the optional parameters to the
// ArtifactsService.Search method.
type ArtifactSearchOptions struct {
	Query string `url:"query,omitempty"`
	Scope string `url:"scope,omitempty"`
}

// Accepts a slice of artifacts, and creates them on Buildkite as a batch.
func (as *ArtifactsService) Create(jobId string, artifacts []*Artifact) ([]*Artifact, *Response, error) {
	u := fmt.Sprintf("v2/jobs/%s/artifacts", jobId)

	req, err := as.client.NewRequest("POST", u, artifacts)
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

// Updates a paticular artifact
func (as *ArtifactsService) Update(jobId string, artifact *Artifact) (*Artifact, *Response, error) {
	u := fmt.Sprintf("v2/jobs/%s/artifacts/%s", jobId, artifact.ID)

	req, err := as.client.NewRequest("PUT", u, artifact)
	if err != nil {
		return nil, nil, err
	}

	a := new(Artifact)
	resp, err := as.client.Do(req, a)
	if err != nil {
		return nil, resp, err
	}

	return a, resp, err
}

// Searches Buildkite for a set of artifacts
func (as *ArtifactsService) Search(buildId string, opt *ArtifactSearchOptions) ([]*Artifact, *Response, error) {
	u := fmt.Sprintf("v2/builds/%s/artifacts/search", buildId)
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
