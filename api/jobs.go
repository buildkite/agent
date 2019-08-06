package api

import (
	"fmt"
)

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

// GetJobState returns the state of a given job
func (c *Client) GetJobState(id string) (*JobState, *Response, error) {
	u := fmt.Sprintf("jobs/%s", id)

	req, err := c.newRequest("GET", u, nil)
	if err != nil {
		return nil, nil, err
	}

	s := new(JobState)
	resp, err := c.doRequest(req, s)
	if err != nil {
		return nil, resp, err
	}

	return s, resp, err
}

// AcceptJob accepts the passed in job. Returns the job with it's finalized set of
// environment variables (when a job is accepted, the agents environment is
// applied to the job)
func (c *Client) AcceptJob(job *Job) (*Job, *Response, error) {
	u := fmt.Sprintf("jobs/%s/accept", job.ID)

	req, err := c.newRequest("PUT", u, nil)
	if err != nil {
		return nil, nil, err
	}

	j := new(Job)
	resp, err := c.doRequest(req, j)
	if err != nil {
		return nil, resp, err
	}

	return j, resp, err
}

// StartJob starts the passed in job
func (c *Client) StartJob(job *Job) (*Response, error) {
	u := fmt.Sprintf("jobs/%s/start", job.ID)

	req, err := c.newRequest("PUT", u, &jobStartRequest{
		StartedAt: job.StartedAt,
	})
	if err != nil {
		return nil, err
	}

	return c.doRequest(req, nil)
}

// FinishJob finishes the passed in job
func (c *Client) FinishJob(job *Job) (*Response, error) {
	u := fmt.Sprintf("jobs/%s/finish", job.ID)

	req, err := c.newRequest("PUT", u, &jobFinishRequest{
		FinishedAt:        job.FinishedAt,
		ExitStatus:        job.ExitStatus,
		ChunksFailedCount: job.ChunksFailedCount,
	})
	if err != nil {
		return nil, err
	}

	return c.doRequest(req, nil)
}

// JobUpdate represents a change request to a job
type JobUpdate struct {
	UUID      string `json:"uuid,omitempty"`
	Attribute string `json:"attribute,omitempty"`
	Value     string `json:"value,omitempty"`
	Append    bool   `json:"append,omitempty"`
}

// JobUpdate updates a job
func (c *Client) JobUpdate(jobId string, jobUpdate *JobUpdate) (*Response, error) {
	u := fmt.Sprintf("jobs/%s", jobId)

	req, err := c.newRequest("PUT", u, jobUpdate)
	if err != nil {
		return nil, err
	}

	return c.doRequest(req, nil)
}

// StepUpdate represents a change request to a step
type StepUpdate struct {
	UUID      string `json:"uuid,omitempty"`
	Attribute string `json:"attribute,omitempty"`
	Value     string `json:"value,omitempty"`
	Append    bool   `json:"append,omitempty"`
}

// StepUpdate updates a step
func (c *Client) StepUpdate(stepId string, stepUpdate *StepUpdate) (*Response, error) {
	u := fmt.Sprintf("steps/%s", stepId)

	req, err := c.newRequest("PUT", u, stepUpdate)
	if err != nil {
		return nil, err
	}

	return c.doRequest(req, nil)
}
