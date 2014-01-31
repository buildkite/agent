package buildbox

import (
  "fmt"
)

type Job struct {
  ID string
  State string
  ScriptPath string `json:"script_path"`
  Output string `json:"output"`
}

func (b Job) String() string {
  return fmt.Sprintf("Job{ID: %s, State: %s, ScriptPath: %s}", b.ID, b.State, b.ScriptPath)
}

func (c *Client) JobNext() (*Job, error) {
  // Create a new instance of a job that will be populated
  // by the client.
  var job Job

  // Return the job.
  return &job, c.Get(&job, "jobs/next")
}

func (c *Client) JobUpdate(job *Job) (*Job, error) {
  // Create a new instance of a job that will be populated
  // with the updated data by the client
  var updatedJob Job

  // Return the job.
  return &updatedJob, c.Put(&updatedJob, "jobs/" + job.ID, job)
}
