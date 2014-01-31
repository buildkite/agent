package buildbox

import (
  "fmt"
)

type Job struct {
  ID string
  State string
  ScriptPath string `json:"script_path"`
  Output string
}

func (b Job) String() string {
  return fmt.Sprintf("Job{ID: %s, State: %s, ScriptPath: %s}", b.ID, b.State, b.ScriptPath)
}

func (c *Client) GetNextJob() (*Job, error) {
  // Create a new instance of a job that will be populated
  // by the client.
  var job Job

  // Return the job.
  return &job, c.Get(&job, "jobs/next")
}
