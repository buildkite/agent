package buildbox

import (
  "fmt"
  "log"
  "os"
  "path/filepath"
  "time"
)

// The Job struct uses strings for StartedAt and FinishedAt because
// if they were actual date objects, then when this struct is
// initialized they would have a default value of: 00:00:00.000000000.
// This causes problems for the Buildbox Agent API because it looks for
// the presence of values in these properties to determine if the build
// has finished.
type Job struct {
  ID string
  State string
  ScriptPath string `json:"script_path"`
  Output string `json:"output,omitempty"`
  ExitStatus string `json:"exit_status,omitempty"`
  StartedAt string `json:"started_at,omitempty"`
  FinishedAt string `json:"finished_at,omitempty"`
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

func (j *Job) Run(client *Client) error {
  // Define the path to the job and ensure it exists
  path, _ := filepath.Abs("tmp") // Joins the current working directory
  err := os.MkdirAll(path, 0700)
  if err != nil {
    log.Fatal(err)
  }

  // Define the ENV variables that should be used for
  // the script
  env := []string{
    fmt.Sprintf("BUILDBOX_BUILD_PATH=%s", path),
    "BUILDBOX_COMIMT=af8f05c9921946dfb502461dad3f5f7335004935",
    "BUILDBOX_REPO=git@github.com:buildboxhq/rails-example.git"}

  // Mark the build as started
  j.StartedAt = time.Now().Format(time.RFC3339)
  client.JobUpdate(j)

  // Run the boostrapping script
  process, _ := runJobScript(j, client, env, ".", "bootstrap.sh")

  // Only progress to the next step if the bootstrapping step
  // was successful
  if process.Success {
    process, _ = runJobScript(j, client, env, "tmp", j.ScriptPath)
  }

  // Mark the build as finished
  j.FinishedAt = time.Now().Format(time.RFC3339)

  // Use the last processes exit status. ExitStatus is a string
  // on the Job struct because 0 is considerered an empty value
  // and won't be marshalled. We only want to send the exit status
  // when the build has finsihed.
  j.ExitStatus = fmt.Sprintf("%d", process.ExitStatus)

  // Finally tell buildbox that we finished the build!
  client.JobUpdate(j)

  return nil
}

func runJobScript(j *Job, client *Client, env []string, path string, script string) (*Process, error) {
  // Store the existing output. Any new data that they
  // get from the process we append to this.
  existingOutput := j.Output

  callback := func(process Process) {
    j.Output = existingOutput + process.Output

    // Post the update to the API
    _, err := client.JobUpdate(j)
    if err != nil {
      log.Fatal(err)
    }
  }

  // Run the bootstrap script
  process, err := RunScript(path, script, env, callback)
  if err != nil {
    log.Fatal(err)
  }

  // Store the final output
  j.Output = existingOutput + process.Output

  return process, err
}
