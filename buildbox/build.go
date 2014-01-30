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
