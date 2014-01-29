package main

import (
  "fmt"
  // "time"
  "github.com/kr/pty"
  // "github.com/buildboxhq/buildbox-agent/buildbox"
  "os/exec"
  "path/filepath"
  "log"
)

const CMD_IOBUF_LEN = 512

func main() {
  // b := buildbox.GetNextBuild()
  // if b == nil {
    // fmt.Println("No build")
  // }

  // time.Sleep(5000 * time.Millisecond)

  absolutePath, _ := filepath.Abs("test/script.sh")
  c := exec.Command(absolutePath)
  c.Dir = "test"
  c.Env = []string{"BUILDBOX_COMIMT=1"}

  fmt.Println("before")

  pty, err := pty.Start(c)
  if err != nil {
    log.Fatal(err)
  }

  fmt.Println("%s", pty)

  err = c.Wait()
  if err != nil {
    log.Fatal(err)
  }

  fmt.Println("%s", c.ProcessState)
}
