package main

import (
  "fmt"
  "log"
  "os"
  "github.com/buildboxhq/buildbox-agent/buildbox"
)

func main() {
  build := buildbox.GetNextBuild()
  if build == nil {
    log.Println("Nothing to do")
    os.Exit(0)
  }

  log.Println(build)

  // This callback will get called every second with the
  // entire output of the command.
  callback := func(process buildbox.Process) {
    fmt.Println(process)
    fmt.Println(process.Output)
  }

  // Define the path to the build and ensure it exists
  path := "/Users/keithpitt/Code/go/src/github.com/buildboxhq/buildbox-agent/tmp"
  err := os.MkdirAll(path, 0700)

  // Define the ENV variables that should be used for
  // the script
  env := []string{
    fmt.Sprintf("BUILDBOX_BUILD_PATH=%s", path),
    "BUILDBOX_COMIMT=59d4685a87db18f5623b478d68cb78a577f4c19f",
    "BUILDBOX_REPO=git@github.com:buildboxhq/rails-example.git"}

  // Run the script
  err = buildbox.RunScript("test/bootstrap.sh", env, callback)
  if err != nil {
    log.Fatal(err)
  }
}
