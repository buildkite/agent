package api

// JobsService handles communication with the job related methods of the
// Buildkite Agent API.
type JobsService struct {
	client *Client
}

// Job represents a Buildkite Agent API Job
type Job struct {
	ID                 string            `json:"id"`
	State              string            `json:"state"`
	Env                map[string]string `json:"env"`
	ChunksMaxSizeBytes int               `json:"chunks_max_size_bytes,omitempty"`
	ExitStatus         string            `json:"exit_status,omitempty"`
	StartedAt          string            `json:"started_at,omitempty"`
	FinishedAt         string            `json:"finished_at,omitempty"`
	ChunksFailedCount  int               `json:"chunks_failed_count"`
}
