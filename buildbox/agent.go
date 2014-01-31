package buildbox

import (
  "fmt"
  "log"
  "os"
  "path/filepath"
)

type Agent struct {
  // The client the agent will use to communicate to
  // the API
  Client Client
}

func (a Agent) Work() {
  for {
    // Try and find some work to do
    job, err := a.Client.GetNextJob()
    if err != nil {
      log.Fatal(err)
    }

    // If there's no ID, then there's no job.
    if job.ID == "" {
      break
    }

    a.run(job)
  }
}

func (a Agent) run(job *Job) {
  log.Printf("%s", job)

  //log.Println(job)

  // This callback will get called every second with the
  // entire output of the command.
  callback := func(process Process) {
    fmt.Println(process)
    // fmt.Println(process.Output)
  }

  // Define the path to the job and ensure it exists
  path, _ := filepath.Abs("tmp") // Joins the current working directory
  err := os.MkdirAll(path, 0700)

  // Define the ENV variables that should be used for
  // the script
  env := []string{
    fmt.Sprintf("BUILDBOX_BUILD_PATH=%s", path),
    "BUILDBOX_COMIMT=af8f05c9921946dfb502461dad3f5f7335004935",
    "BUILDBOX_REPO=git@github.com:buildboxhq/rails-example.git"}

  // Run the bootstrap script
  err = RunScript(".", "bootstrap.sh", env, callback)
  if err != nil {
    log.Fatal(err)
  }

  // Now run the build script
  err = RunScript("tmp", job.ScriptPath, env, callback)
  if err != nil {
    log.Fatal(err)
  }
}
