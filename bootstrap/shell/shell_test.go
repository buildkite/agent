package shell_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
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

func TestDefaultWorkingDirFromSystem(t *testing.T) {
	sh, err := shell.New()
	if err != nil {
		t.Fatal(err)
	}

	currentWd, _ := os.Getwd()
	if actual := sh.Getwd(); actual != currentWd {
		t.Fatalf("Expected working dir %q, got %q", currentWd, actual)
	}
}

func TestWorkingDir(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "shelltest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// macos has a symlinked temp dir
	tempDir, _ = filepath.EvalSymlinks(tempDir)
	dirs := []string{tempDir, "my", "test", "dirs"}

	if err := os.MkdirAll(filepath.Join(dirs...), 0700); err != nil {
		t.Fatal(err)
	}

	currentWd, _ := os.Getwd()

	sh, err := shell.New()
	sh.Logger = shell.DiscardLogger

	if err != nil {
		t.Fatal(err)
	}

	for idx := range dirs {
		dir := filepath.Join(dirs[0 : idx+1]...)

		if err := sh.Chdir(dir); err != nil {
			t.Fatal(err)
		}

		if actual := sh.Getwd(); actual != dir {
			t.Fatalf("Expected working dir %q, got %q", dir, actual)
		}

		out, err := sh.RunAndCapture("pwd")
		if err != nil {
			t.Fatal(err)
		}

		if actual := out; actual != dir {
			t.Fatalf("Expected working dir (from pwd command) %q, got %q", dir, actual)
		}
	}

	afterWd, _ := os.Getwd()
	if afterWd != currentWd {
		t.Fatalf("Expected working dir to be the same as before shell commands ran")
	}
}
