package api

import (
	"context"
	"errors"
	"fmt"
)

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

// MetaDataBatch represents a batch of key/value pairs for the set-batch endpoint.
type MetaDataBatch struct {
	Items []MetaData `json:"items"`
	Scope string     `json:"scope,omitempty"`
}

// Sets the meta data value
func (c *Client) SetMetaData(ctx context.Context, jobId string, metaData *MetaData) (*Response, error) {
	u := fmt.Sprintf("jobs/%s/data/set", railsPathEscape(jobId))

	req, err := c.newRequest(ctx, "POST", u, metaData)
	if err != nil {
		return nil, err
	}

	return c.doRequest(req, nil)
}

// SetMetaDataBatch sets multiple meta data key/value pairs in a single request.
// The operation is transactional: all items succeed or none do.
func (c *Client) SetMetaDataBatch(ctx context.Context, jobId string, batch *MetaDataBatch) (*Response, error) {
	u := fmt.Sprintf("jobs/%s/data/set-batch", railsPathEscape(jobId))

	req, err := c.newRequest(ctx, "POST", u, batch)
	if err != nil {
		return nil, err
	}

	return c.doRequest(req, nil)
}

// GetMetaData reads a value. targetScope selects the job or build URL;
// metadataScope selects the store (empty means build, "job" means job).
func (c *Client) GetMetaData(ctx context.Context, targetScope, id, key, metadataScope string) (*MetaData, *Response, error) {
	if targetScope != "job" && targetScope != "build" {
		return nil, nil, errors.New("scope must either be job or build")
	}

	u := fmt.Sprintf("%ss/%s/data/get", targetScope, railsPathEscape(id))
	m := &MetaData{Key: key, Scope: metadataScope}

	req, err := c.newRequest(ctx, "POST", u, m)
	if err != nil {
		return nil, nil, err
	}

	resp, err := c.doRequest(req, m)
	if err != nil {
		return nil, resp, err
	}

	return m, resp, err
}

// ExistsMetaData checks whether a key has been set. targetScope selects the
// job or build URL; metadataScope selects the store (empty means build).
func (c *Client) ExistsMetaData(ctx context.Context, targetScope, id, key, metadataScope string) (*MetaDataExists, *Response, error) {
	if targetScope != "job" && targetScope != "build" {
		return nil, nil, errors.New("scope must either be job or build")
	}

	u := fmt.Sprintf("%ss/%s/data/exists", targetScope, railsPathEscape(id))
	m := &MetaData{Key: key, Scope: metadataScope}

	req, err := c.newRequest(ctx, "POST", u, m)
	if err != nil {
		return nil, nil, err
	}

	e := new(MetaDataExists)
	resp, err := c.doRequest(req, e)
	if err != nil {
		return nil, resp, err
	}

	return e, resp, err
}

// MetaDataKeys lists keys. targetScope selects the job or build URL;
// metadataScope selects the store (empty means build, "job" means job).
func (c *Client) MetaDataKeys(ctx context.Context, targetScope, id, metadataScope string) ([]string, *Response, error) {
	if targetScope != "job" && targetScope != "build" {
		return nil, nil, errors.New("scope must either be job or build")
	}

	u := fmt.Sprintf("%ss/%s/data/keys", targetScope, railsPathEscape(id))
	var body any
	if metadataScope != "" {
		body = &MetaData{Scope: metadataScope}
	}

	req, err := c.newRequest(ctx, "POST", u, body)
	if err != nil {
		return nil, nil, err
	}

	keys := []string{}
	resp, err := c.doRequest(req, &keys)
	if err != nil {
		return nil, resp, err
	}

	return keys, resp, err
}
