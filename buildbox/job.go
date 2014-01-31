package buildbox

import (
  "fmt"
)

type Job struct {
  State string
  Script string
  Output string
}

func (b Job) String() string {
  return fmt.Sprintf("Job{State: %s}", b.State)
}

func (c *Client) GetNextJob() (*Job, error) {
  // Create a new instance of a job that will be populated
  // by the client.
  var job Job

  // Return the job.
  return &job, c.Get(&job, "jobs/next")
}
