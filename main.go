package main

import (
  "fmt"
  "log"
  "os"
  "github.com/buildboxhq/buildbox-agent/buildbox"
  "path/filepath"
)

func main() {
  //build := buildbox.GetNextBuild()
  //if build == nil {
  //  log.Println("Nothing to do")
  //  os.Exit(0)
  //}

  //log.Println(build)

  // This callback will get called every second with the
  // entire output of the command.
  callback := func(process buildbox.Process) {
    fmt.Println(process)
    // fmt.Println(process.Output)
  }

  // Define the path to the build and ensure it exists
  path, _ := filepath.Abs("tmp") // Joins the current working directory
  err := os.MkdirAll(path, 0700)

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
  err = buildbox.RunScript("tmp", "script/buildbox", env, callback)
  if err != nil {
    log.Fatal(err)
  }
}
