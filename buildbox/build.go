package buildbox

import (
  "fmt"
)

type Build struct {
  State string
  Script string
  Output string
}

func (b Build) String() string {
  return fmt.Sprintf("Build{State: %s}", b.State)
}

func (c *Client) GetNextBuild() (*Build, error) {
  // Create a new instance of a build that will be populated
  // by the client.
  var build Build

  // Return the build.
  return &build, c.Get(&build, "builds/queue/next")
}
