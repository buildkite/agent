package jobapi

import (
	"sort"

	"github.com/buildkite/agent/v3/internal/socket"
)

// ErrorResponse is the response body for any errors that occur.
type ErrorResponse = socket.ErrorResponse

// EnvGetResponse is the response body for the GET /env endpoint
type EnvGetResponse struct {
	Env map[string]string `json:"env"` // Different to EnvUpdateRequest because we don't want to send nulls
}

// EnvUpdateRequest is the request body for the PATCH /env endpoint
type EnvUpdateRequest struct {
	Env map[string]string `json:"env"`
}

// EnvUpdateRequestPayload is the request body that the PATCH /env endpoint unmarshalls requests into
type EnvUpdateRequestPayload struct {
	Env map[string]*string `json:"env"`
}

// EnvUpdateResponse is the response body for the PATCH /env endpoint
type EnvUpdateResponse struct {
	Added   []string `json:"added"`
	Updated []string `json:"updated"`
}

func (e EnvUpdateResponse) Normalize() {
	sort.Strings(e.Added)
	sort.Strings(e.Updated)
}

// EnvDeleteRequest is the request body for the DELETE /env endpoint
type EnvDeleteRequest struct {
	Keys []string `json:"keys"`
}

// EnvDeleteResponse is the response body for the DELETE /env endpoint
type EnvDeleteResponse struct {
	Deleted []string `json:"deleted"`
}

func (e EnvDeleteResponse) Normalize() {
	sort.Strings(e.Deleted)
}

// RedactionCreateRequest is the request body for the POST /redactions endpoint
type RedactionCreateRequest struct {
	Redact string `json:"redact"`
}

// RedactionCreateResponse is the response body for the POST /redactions endpoint
type RedactionCreateResponse struct {
	Redacted string `json:"redacted"`
}
