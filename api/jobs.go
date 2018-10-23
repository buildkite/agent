package api

import (
	"fmt"
)

// JobsService handles communication with the job related methods of the
// Buildkite Agent API.
type JobsService struct {
	client *Client
}

// Job represents a Buildkite Agent API Job
type Job struct {
	ID                 string            `json:"id,omitempty"`
	Endpoint           string            `json:"endpoint"`
	State              string            `json:"state,omitempty"`
	Env                map[string]string `json:"env,omitempty"`
	ChunksMaxSizeBytes int               `json:"chunks_max_size_bytes,omitempty"`
	ExitStatus         string            `json:"exit_status,omitempty"`
	StartedAt          string            `json:"started_at,omitempty"`
	FinishedAt         string            `json:"finished_at,omitempty"`
	ChunksFailedCount  int               `json:"chunks_failed_count,omitempty"`
}

type JobState struct {
	State string `json:"state,omitempty"`
}

type jobStartRequest struct {
	StartedAt string `json:"started_at,omitempty"`
}

type jobFinishRequest struct {
	ExitStatus        string `json:"exit_status,omitempty"`
	FinishedAt        string `json:"finished_at,omitempty"`
	ChunksFailedCount int    `json:"chunks_failed_count"`
}

// Fetches a job
func (js *JobsService) GetState(id string) (*JobState, *Response, error) {
	u := fmt.Sprintf("jobs/%s", id)

	req, err := js.client.NewRequest("GET", u, nil)
	if err != nil {
		return nil, nil, err
	}

	s := new(JobState)
	resp, err := js.client.Do(req, s)
	if err != nil {
		return nil, resp, err
	}

	return s, resp, err
}

// Accepts the passed in job. Returns the job with it's finalized set of
// environment variables (when a job is accepted, the agents environment is
// applied to the job)
func (js *JobsService) Accept(job *Job) (*Job, *Response, error) {
	u := fmt.Sprintf("jobs/%s/accept", job.ID)

	req, err := js.client.NewRequest("PUT", u, nil)
	if err != nil {
		return nil, nil, err
	}

	j := new(Job)
	resp, err := js.client.Do(req, j)
	if err != nil {
		return nil, resp, err
	}

	return j, resp, err
}

// Starts the passed in job
func (js *JobsService) Start(job *Job) (*Response, error) {
	u := fmt.Sprintf("jobs/%s/start", job.ID)

	req, err := js.client.NewRequest("PUT", u, &jobStartRequest{
		StartedAt: job.StartedAt,
	})
	if err != nil {
		return nil, err
	}

	return js.client.Do(req, nil)
}

// Finishes the passed in job
func (js *JobsService) Finish(job *Job) (*Response, error) {
	u := fmt.Sprintf("jobs/%s/finish", job.ID)

	req, err := js.client.NewRequest("PUT", u, &jobFinishRequest{
		FinishedAt:        job.FinishedAt,
		ExitStatus:        job.ExitStatus,
		ChunksFailedCount: job.ChunksFailedCount,
	})
	if err != nil {
		return nil, err
	}

	return js.client.Do(req, nil)
}

// Updates a step
func (js *JobsService) StepUpdate(jobId string, stepUpdate *StepUpdate) (*Response, error) {
	u := fmt.Sprintf("jobs/%s/step_update", jobId)

	req, err := js.client.NewRequest("PUT", u, stepUpdate)
	if err != nil {
		return nil, err
	}

	return js.client.Do(req, nil)
}
