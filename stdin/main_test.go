package stdin_test

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"runtime"
	"testing"

	"github.com/buildkite/agent/stdin"
)

// Derived from TestStatStdin in https://golang.org/src/os/os_test.go

func TestMain(m *testing.M) {
	switch os.Getenv("GO_WANT_HELPER_PROCESS") {
	case "":
		// Normal test mode
		os.Exit(m.Run())

	case "1":
		fmt.Printf("%v", stdin.IsReadable())
		os.Exit(0)
	}
}

func TestIsStdinIsNotReadableByDefault(t *testing.T) {
	var cmd *exec.Cmd
	if runtime.GOOS == `windows` {
		cmd = exec.Command("cmd", "/c", os.Args[0])
	} else {
		cmd = exec.Command("/bin/sh", "-c", os.Args[0])
	}
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	cmd.Stdin = nil

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to spawn child process: %v %q", err, string(output))
	}

	if g, e := string(output), "false"; g != e {
		t.Errorf("Stdin should not be readable, wanted %q, got %q", e, g)
	}
}

func TestIsStdinIsReadableWithAPipe(t *testing.T) {
	var cmd *exec.Cmd
	if runtime.GOOS == `windows` {
		cmd = exec.Command("cmd", "/c", `echo output | `+os.Args[0])
	} else {
		cmd = exec.Command("/bin/sh", "-c", `echo output | `+os.Args[0])
	}
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to spawn child process: %v %q", err, string(output))
	}

	if g, e := string(output), "true"; g != e {
		t.Errorf("Stdin should be readable from a pipe, wanted %q, got %q", e, g)
	}
}

func TestIsStdinIsReadableWithOutputRedirection(t *testing.T) {
	tmpfile, err := ioutil.TempFile("", "output-redirect")
	if err != nil {
		log.Fatal(err)
	}

	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte("output")); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	var cmd *exec.Cmd
	if runtime.GOOS == `windows` {
		cmd = exec.Command("cmd", "/c", os.Args[0]+`< `+tmpfile.Name())
	} else {
		cmd = exec.Command("/bin/sh", "-c", os.Args[0]+`< `+tmpfile.Name())
	}
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to spawn child process: %v %q", err, string(output))
	}

	if g, e := string(output), "true"; g != e {
		t.Errorf("Stdin should be readable from a file, wanted %q, got %q", e, g)
	}
}
