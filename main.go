package main

import (
	"fmt"
	// "time"
	"github.com/kr/pty"
	// "github.com/buildboxhq/buildbox-agent/buildbox"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

const CMD_IOBUF_LEN = 512

func main() {
	// b := buildbox.GetNextBuild()
	// if b == nil {
	// fmt.Println("No build")
	// }

	// time.Sleep(5000 * time.Millisecond)

	// so run this script
	absolutePath, _ := filepath.Abs("test/script.sh")
	c := exec.Command(absolutePath)
	// with cwd this folder
	c.Dir = "test"
	// inject env stuff for build box
	c.Env = []string{"BUILDBOX_COMIMT=1"}

	// before the process is run
	fmt.Println("before")

	// spawn a pty
	pty, err := pty.Start(c)

	// probably not this
	if err != nil {
		log.Fatal(err)
	}
	// not really much use but yeah it is a PTY..
	fmt.Printf("%v\n", pty)

	// now we attach the output of the pty to stdout
	// need a line buffered go routine sending
	// this to buildbox..
	io.Copy(os.Stdout, pty)

	// wait for the pty to finish
	err = c.Wait()

	// probably not this but something telling us the exit result
	if err != nil {
		log.Fatalf("left the building %s", err)
	}

	// never gets run because fatal is fatal..
	fmt.Println("status of the process %s", c.ProcessState)
}
