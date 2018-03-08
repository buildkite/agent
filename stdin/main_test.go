package stdin_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/buildkite/agent/stdin"
)

func TestMain(m *testing.M) {
	switch os.Getenv("GO_TEST_MODE") {
	case "":
		// Normal test mode
		os.Exit(m.Run())

	case "stdin_check":
		fmt.Printf("%v", stdin.IsReadable())
		os.Exit(0)
	}
}

func TestIsStdinIsNotReadable(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = []string{"GO_TEST_MODE=stdin_check"}
	cmd.Stdin = nil

	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	if g, e := string(output), "false"; g != e {
		t.Errorf("stdin_check: want %q, got %q", e, g)
	}
}

func TestIsStdinIsReadable(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = []string{"GO_TEST_MODE=stdin_check"}
	cmd.Stdin = bytes.NewBufferString("llamas")

	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	if g, e := string(output), "true"; g != e {
		t.Errorf("stdin_check: want %q, got %q", e, g)
	}
}
