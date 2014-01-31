package main

import (
  "fmt"
  "log"
  "os"
  "github.com/buildboxhq/buildbox-agent/buildbox"
  "path/filepath"
)

func main() {
  var client buildbox.Client
  client.AgentAccessToken = "e6296371ed3dd3f24881b0866506b8c6"
  client.URL = "http://agent.buildbox.dev/v1"

  job, err := client.GetNextJob()
  if err != nil {
    log.Fatal(err)
  }

  log.Printf("%s", job)

  //log.Println(job)

  // This callback will get called every second with the
  // entire output of the command.
  callback := func(process buildbox.Process) {
    fmt.Println(process)
    // fmt.Println(process.Output)
  }

  // Define the path to the job and ensure it exists
  path, _ := filepath.Abs("tmp") // Joins the current working directory
  err = os.MkdirAll(path, 0700)

  // Define the ENV variables that should be used for
  // the script
  env := []string{
    fmt.Sprintf("BUILDBOX_BUILD_PATH=%s", path),
    "BUILDBOX_COMIMT=af8f05c9921946dfb502461dad3f5f7335004935",
    "BUILDBOX_REPO=git@github.com:buildboxhq/rails-example.git"}

  // Run the bootstrap script
  err = buildbox.RunScript(".", "bootstrap.sh", env, callback)
  if err != nil {
    log.Fatal(err)
  }

  // Now run the build script
  err = buildbox.RunScript("tmp", job.ScriptPath, env, callback)
  if err != nil {
    log.Fatal(err)
  }
}
