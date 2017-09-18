package shell_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/buildkite/agent/bootstrap/shell"
	"github.com/lox/bintest/proxy"
)

func TestRunAndCaptureWithTTY(t *testing.T) {
	sshKeygen, err := proxy.New("ssh-keygen")
	if err != nil {
		t.Fatal(err)
	}
	defer sshKeygen.Close()

	sh, err := shell.New()
	if err != nil {
		t.Fatal(err)
	}

	sh.PTY = true

	go func() {
		call := <-sshKeygen.Ch
		fmt.Fprintln(call.Stdout, "Llama party! 🎉")
		call.Exit(0)
	}()

	actual, err := sh.RunAndCapture(sshKeygen.Path, "-f", "my_hosts", "-F", "llamas.com")
	if err != nil {
		t.Error(err)
	}

	if expected := "Llama party! 🎉"; string(actual) != expected {
		t.Fatalf("Expected %q, got %q", expected, actual)
	}
}

func TestRun(t *testing.T) {
	sshKeygen, err := proxy.New("ssh-keygen")
	if err != nil {
		t.Fatal(err)
	}
	defer sshKeygen.Close()

	sh, err := shell.New()
	if err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}

	sh.PTY = false
	sh.Writer = out
	sh.Logger = &shell.WriterLogger{Writer: out, Ansi: false}

	go func() {
		call := <-sshKeygen.Ch
		fmt.Fprintln(call.Stdout, "Llama party! 🎉")
		call.Exit(0)
	}()

	if err = sh.Run(sshKeygen.Path, "-f", "my_hosts", "-F", "llamas.com"); err != nil {
		t.Fatal(err)
	}

	actual := out.String()

	if expected := "$ " + sshKeygen.Path + " -f my_hosts -F llamas.com\nLlama party! 🎉\n"; actual != expected {
		t.Fatalf("Expected %q, got %q", expected, actual)
	}
}
