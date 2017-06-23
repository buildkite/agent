package api

import (
	"fmt"
)

// MetaDataService handles communication with the meta data related methods of
// the Buildkite Agent API.
type MetaDataService struct {
	client *Client
}

// DefaultMetadataScope is the default scope used for metadata
const DefaultMetadataScope = "build"

// MetaData represents a Buildkite Agent API MetaData
type MetaData struct {
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
	Scope string `json:"scope,omitempty"`
}

// MetaDataExists represents a Buildkite Agent API MetaData Exists check
// response
type MetaDataExists struct {
	Exists bool `json:"exists"`
}

// Sets the meta data value
func (ps *MetaDataService) Set(jobId string, metaData *MetaData) (*Response, error) {
	u := fmt.Sprintf("jobs/%s/data/set", jobId)

	req, err := ps.client.NewRequest("POST", u, metaData)
	if err != nil {
		return nil, err
	}

	return ps.client.Do(req, nil)
}

// Gets the meta data value
func (ps *MetaDataService) Get(jobId string, key string, scope string) (*MetaData, *Response, error) {
	u := fmt.Sprintf("jobs/%s/data/get", jobId)
	m := &MetaData{Key: key, Scope: scope}

	req, err := ps.client.NewRequest("POST", u, m)
	if err != nil {
		return nil, nil, err
	}

	resp, err := ps.client.Do(req, m)
	if err != nil {
		return nil, resp, err
	}

	return m, resp, err
}

// Returns true if the meta data key has been set, false if it hasn't.
func (ps *MetaDataService) Exists(jobId string, key string, scope string) (*MetaDataExists, *Response, error) {
	u := fmt.Sprintf("jobs/%s/data/exists", jobId)
	m := &MetaData{Key: key, Scope: scope}

	req, err := ps.client.NewRequest("POST", u, m)
	if err != nil {
		return nil, nil, err
	}

	e := new(MetaDataExists)
	resp, err := ps.client.Do(req, e)
	if err != nil {
		return nil, resp, err
	}

	return e, resp, err
}
