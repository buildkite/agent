package agentapi

import "time"

// PingResponse is the response body for the ping endpoint.
type PingResponse struct {
	Now time.Time `json:"now"`
}

// ValueResponse is the response body for endpoints that return a single value.
type ValueResponse struct {
	Value string `json:"value"`
}

// LockCASRequest is the request body for the PATCH /lock/{key} endpoint.
type LockCASRequest struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// LockCASResponse is the response body for the PATCH /lock/{key} endpoint.
type LockCASResponse struct {
	Value   string `json:"value"`
	Swapped bool   `json:"swapped"`
}
