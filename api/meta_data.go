package api

import (
	"fmt"
)

// MetaDataService handles communication with the meta data related methods of
// the Buildkite Agent API.
type MetaDataService struct {
	client *Client
}

// MetaData represents a Buildkite Agent API MetaData
type MetaData struct {
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
}

// Sets the meta data value
func (ps *MetaDataService) Set(jobId string, metaData *MetaData) (*MetaData, *Response, error) {
	u := fmt.Sprintf("v2/jobs/%s/data/set", jobId)

	req, err := ps.client.NewRequest("POST", u, metaData)
	if err != nil {
		return nil, nil, err
	}

	m := new(MetaData)
	resp, err := ps.client.Do(req, m)
	if err != nil {
		return nil, resp, err
	}

	return m, resp, err
}

// Gets the meta data value
func (ps *MetaDataService) Get(jobId string, key string) (*MetaData, *Response, error) {
	u := fmt.Sprintf("v2/jobs/%s/data/get", jobId)
	m := &MetaData{Key: key}

	req, err := ps.client.NewRequest("POST", u, m)
	if err != nil {
		return nil, nil, err
	}

	metaData := new(MetaData)
	resp, err := ps.client.Do(req, metaData)
	if err != nil {
		return nil, resp, err
	}

	return metaData, resp, err
}
