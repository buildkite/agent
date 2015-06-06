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
	ID                 string            `json:"id"`
	State              string            `json:"state"`
	Env                map[string]string `json:"env"`
	ChunksMaxSizeBytes int               `json:"chunks_max_size_bytes,omitempty"`
	ExitStatus         string            `json:"exit_status,omitempty"`
	StartedAt          string            `json:"started_at,omitempty"`
	FinishedAt         string            `json:"finished_at,omitempty"`
	ChunksFailedCount  int               `json:"chunks_failed_count"`
}

// Fetches a job
func (js *JobsService) Get(id string) (*Job, *Response, error) {
	u := fmt.Sprintf("v2/jobs/%s", id)

	req, err := js.client.NewRequest("GET", u, nil)
	if err != nil {
		return nil, nil, err
	}

	job := new(Job)
	resp, err := js.client.Do(req, job)
	if err != nil {
		return nil, resp, err
	}

	return job, resp, err
}

// Accepts the passed in job
func (js *JobsService) Accept(job *Job) (*Job, *Response, error) {
	u := fmt.Sprintf("v2/jobs/%s/accept", job.ID)

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
func (js *JobsService) Start(job *Job) (*Job, *Response, error) {
	u := fmt.Sprintf("v2/jobs/%s/start", job.ID)

	req, err := js.client.NewRequest("PUT", u, job)
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

// Finishes the passed in job
func (js *JobsService) Finish(job *Job) (*Job, *Response, error) {
	u := fmt.Sprintf("v2/jobs/%s/finish", job.ID)

	req, err := js.client.NewRequest("PUT", u, job)
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
