package main

import (
  "fmt"
  "time"
  "github.com/kr/pty"
  // "github.com/buildboxhq/buildbox-agent/buildbox"
  "os/exec"
  "path/filepath"
  "log"
  "io"
  "bytes"
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
  fmt.Println("after")

  var out bytes.Buffer

  go func() {
    fmt.Println("before-copy")
    io.Copy(&out, pty)
    fmt.Println("after-copy")
  }()

  go func(){
    for {
      fmt.Println("--")
      fmt.Println(out.String())
      time.Sleep(1000 * time.Millisecond)
      fmt.Println("--")
    }
  }()


  fmt.Println("gonna wait")
  c.Wait()


  fmt.Println("all done!")
  fmt.Println(out.String())

  fmt.Println("finished with: %s", c.ProcessState)
}
