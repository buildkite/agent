package api

import (
	"fmt"
)

// QueriesService handles communication with the query related methods of the
// Buildkite Agent API.
type QueriesService struct {
	client *Client
}

type QueryError struct {
	Message string `json:"message,omitempty"`
}

// Query represents a Buildkite Agent API GraphQL Query
type Query struct {
	Query     string        `json:"query,omitempty"`
	Data      interface{}   `json:"data,omitempty"`
	ErrorType string        `json:"type,omitempty"`
	Errors    []*QueryError `json:"errors,omitempty"`
}

// Performs a GraphQL query
func (ps *QueriesService) Perform(jobId string, query string) (*Query, *Response, error) {
	u := fmt.Sprintf("jobs/%s/query", jobId)
	m := &Query{Query: query}

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
