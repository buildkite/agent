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

// WorkdirSetRequest is the request body for the PUT /workdir endpoint
type WorkdirSetRequest struct {
	Workdir string `json:"workdir"`
}

// WorkdirSetResponse echoes the absolute working directory.
type WorkdirSetResponse struct {
	Workdir string `json:"workdir"`
}

// RedactionCreateRequest is the request body for the POST /redactions endpoint
type RedactionCreateRequest struct {
	Redact string `json:"redact"`
}

// RedactionCreateResponse is the response body for the POST /redactions endpoint
type RedactionCreateResponse struct {
	Redacted string `json:"redacted"`
}

// PromiseFailureRequest is the request body for the POST /promise-failure endpoint
type PromiseFailureRequest struct {
	ExitStatus int    `json:"exit_status"`
	Reason     string `json:"reason,omitempty"`
}

// Promise failure outcomes, reported in PromiseFailureResponse.Outcome.
const (
	// PromiseFailureDeclared means this call declared the exit status to the
	// Buildkite API.
	PromiseFailureDeclared = "declared"
	// PromiseFailureDebounced means an earlier call already declared this exit
	// status, so this call shared that result without calling the Buildkite API.
	PromiseFailureDebounced = "debounced"
)

// PromiseFailureResponse is the response body for the POST /promise-failure endpoint
type PromiseFailureResponse struct {
	// Outcome is PromiseFailureDeclared or PromiseFailureDebounced.
	Outcome string `json:"outcome"`

	// Accepted reports whether the Buildkite API accepted the promised failure.
	Accepted bool `json:"accepted"`

	// UpstreamStatus is the Buildkite API status, when one was received.
	// Network errors that never received a response leave this as 0.
	UpstreamStatus int `json:"upstream_status,omitempty"`

	// Error is the Buildkite API declaration error, if Accepted is false.
	Error string `json:"error,omitempty"`
}
