package main

import (
	"fmt"
	// "time"
	"github.com/kr/pty"
	// "github.com/buildboxhq/buildbox-agent/buildbox"
	"bufio"
	//	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

const CMD_IOBUF_LEN = 512

func process(wg *sync.WaitGroup, batch []string) {
	defer wg.Done()
	// send to API
	log.Printf("shipping %d", len(batch))
}

// low batch size for demo purposes
const BatchSize = 4

func main() {
	// b := buildbox.GetNextBuild()
	// if b == nil {
	// fmt.Println("No build")
	// }

	// time.Sleep(5000 * time.Millisecond)

	// process waiter
	wg := new(sync.WaitGroup)

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
	// read each line using a buffered reader
	scanner := bufio.NewScanner(pty)

	// Batch the lines
	var batch []string

	for scanner.Scan() {
		batch = append(batch, scanner.Text())
		// buffer is full ship it
		if len(batch) == BatchSize {
			wg.Add(1)

			// process this in another go routine
			go process(wg, batch)

			batch = nil
		}
		fmt.Fprintln(os.Stdout, scanner.Text())
	}

	// PTY has closed the stream..

	if batch != nil {
		// ship any remaining data
		go process(wg, batch)
	}

	// wait for the pty to finish
	err = c.Wait()

	// probably not this but something telling us the exit result
	if err != nil {
		log.Fatalf("left the building %s", err)
	}

	// never gets run because fatal is fatal..
	fmt.Println("status of the process %s", c.ProcessState)
}
