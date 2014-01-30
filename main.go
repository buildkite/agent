package main

import (
  "fmt"
  // "time"
  "github.com/kr/pty"
  // "github.com/buildboxhq/buildbox-agent/buildbox"
  "os/exec"
  "path/filepath"
  "log"
  "io"
  "syscall"
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
  c.Args[0] = "script.sh"
  c.SysProcAttr = &syscall.SysProcAttr{Setctty: true, Setsid: true}

  fmt.Println("before")

  output := new(io.Writer)

  ptyMaster, ptySlave, err := pty.Open()
  if err != nil {
    log.Fatal(err)
  }

  c.Stdout = ptySlave
  c.Stderr = ptySlave

	go func() {
		// defer output.Close()
		log.Printf("startPty: begin of stdout pipe")
		io.Copy(*output, ptyMaster)
		log.Printf("startPty: end of stdout pipe")
	}()

  err = c.Start()
  if err != nil {
    log.Fatal(err)
  }

  err = c.Wait()
  if err != nil {
    log.Fatal(err)
  }

  fmt.Println("%s", c.ProcessState)
}
