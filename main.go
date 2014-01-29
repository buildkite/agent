package main

import (
  "fmt"
  "time"
  "github.com/buildboxhq/buildbox-agent/buildbox"
)

func main() {
  for {
    b := buildbox.GetNextBuild()

    if b != nil {
      fmt.Println(b)
    } else {
      fmt.Println("No build")
    }

    time.Sleep(5000 * time.Millisecond)
  }

  fmt.Printf("Done")
}
