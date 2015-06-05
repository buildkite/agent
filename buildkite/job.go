package buildkite

// The Job struct uses strings for StartedAt and FinishedAt because
// if they were actual date objects, then when this struct is
// initialized they would have a default value of: 00:00:00.000000000.
// This causes problems for the Buildkite Agent API because it looks for
// the presence of values in these properties to determine if the build
// has finished.
type Job struct {
	API API

	ID string

	State string

	Env map[string]string

	ChunksMaxSizeBytes int `json:"chunks_max_size_bytes,omitempty"`

	ExitStatus string `json:"exit_status,omitempty"`

	StartedAt string `json:"started_at,omitempty"`

	FinishedAt string `json:"finished_at,omitempty"`

	ChunksFailedCount int `json:"chunks_failed_count"`
}

func (j *Job) Accept() error {
	return j.API.Put("jobs/"+j.ID+"/accept", &j, j)
}

func (j *Job) Start() error {
	return j.API.Put("jobs/"+j.ID+"/start", &j, j)
}

func (j *Job) Finish() error {
	return j.API.Put("jobs/"+j.ID+"/finish", &j, j, APIInfinityRetires)
}

func (j *Job) Refresh() error {
	return j.API.Get("jobs/"+j.ID, &j)
}
