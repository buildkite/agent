package buildbox

import (
  "fmt"
  "log"
  "os"
  "path/filepath"
)

type Job struct {
  ID string
  State string
  ScriptPath string `json:"script_path"`
  Output string `json:"output,omitempty"`
  ExitStatus string `json:"exit_status,omitempty"`
  StartedAt string
  FinishedAt string
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
  // j.StartedAt = "started"
  client.JobUpdate(j)

  // Run the boostrapping script
  process, _ := runJobScript(j, client, env, ".", "bootstrap.sh")

  // Only progress to the next step if the bootstrapping step
  // was successful
  if process.ExitStatus == 0 {
    process, _ = runJobScript(j, client, env, "tmp", j.ScriptPath)
  }

  // Mark the build as finished
  // j.FinishedAt = "finished"
  client.JobUpdate(j)

  log.Fatal("Done")

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
