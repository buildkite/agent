package shell_test

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/bootstrap/shell"
	"github.com/buildkite/bintest/v3"
	"github.com/stretchr/testify/assert"
)

func TestRunAndCaptureWithTTY(t *testing.T) {
	sshKeygen, err := bintest.CompileProxy("ssh-keygen")
	if err != nil {
		t.Fatal(err)
	}
	defer sshKeygen.Close()

	sh := newShellForTest(t)
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
		t.Errorf("Expected %q, got %q", expected, actual)
	}
}

func TestRunAndCaptureWithExitCode(t *testing.T) {
	sshKeygen, err := bintest.CompileProxy("ssh-keygen")
	if err != nil {
		t.Fatal(err)
	}
	defer sshKeygen.Close()

	sh := newShellForTest(t)

	go func() {
		call := <-sshKeygen.Ch
		fmt.Fprintln(call.Stdout, "Llama drama! 🚨")
		call.Exit(24)
	}()

	_, err = sh.RunAndCapture(sshKeygen.Path)
	if err == nil {
		t.Error("Expected an error, got nil")
	}

	if exitCode := shell.GetExitCode(err); exitCode != 24 {
		t.Fatalf("Expected %d, got %d", 24, exitCode)
	}
}

func TestRun(t *testing.T) {
	sshKeygen, err := bintest.CompileProxy("ssh-keygen")
	if err != nil {
		t.Fatal(err)
	}
	defer sshKeygen.Close()

	out := &bytes.Buffer{}

	sh := newShellForTest(t)
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

	promptPrefix := "$"
	if runtime.GOOS == "windows" {
		promptPrefix = ">"
	}

	if expected := promptPrefix + " " + sshKeygen.Path + " -f my_hosts -F llamas.com\nLlama party! 🎉\n"; actual != expected {
		t.Fatalf("Expected %q, got %q", expected, actual)
	}
}

func TestRunWithStdin(t *testing.T) {
	out := &bytes.Buffer{}
	sh := newShellForTest(t)
	sh.Writer = out

	err := sh.WithStdin(strings.NewReader("hello stdin")).Run("tr", "hs", "HS")
	if err != nil {
		t.Fatal(err)
	}
	if expected, actual := "Hello Stdin", out.String(); expected != actual {
		t.Errorf("expected %q, got %q", expected, actual)
	}
}

func TestContextCancelTerminates(t *testing.T) {
	if runtime.GOOS == `windows` {
		t.Skip("Not supported in windows")
	}

	sleepCmd, err := bintest.CompileProxy("sleep")
	if err != nil {
		t.Fatal(err)
	}
	defer sleepCmd.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sh, err := shell.NewWithContext(ctx)
	if err != nil {
		t.Fatal(err)
	}

	sh.Logger = shell.DiscardLogger

	go func() {
		call := <-sleepCmd.Ch
		time.Sleep(time.Second * 60)
		call.Exit(0)
	}()

	cancel()

	err = sh.Run(sleepCmd.Path)
	if !shell.IsExitSignaled(err) {
		t.Fatalf("Expected signal exit, got %#v", err)
	}
}

func TestInterrupt(t *testing.T) {
	if runtime.GOOS == `windows` {
		t.Skip("Not supported in windows")
	}

	sleepCmd, err := bintest.CompileProxy("sleep")
	if err != nil {
		t.Fatal(err)
	}
	defer sleepCmd.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sh, err := shell.NewWithContext(ctx)
	if err != nil {
		t.Fatal(err)
	}

	sh.Logger = shell.DiscardLogger

	go func() {
		call := <-sleepCmd.Ch
		time.Sleep(time.Second * 10)
		call.Exit(0)
	}()

	// interrupt the process after 50ms
	go func() {
		<-time.After(time.Millisecond * 50)
		sh.Interrupt()
	}()

	err = sh.Run(sleepCmd.Path)
	if err == nil {
		t.Error("Expected an error")
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
	if runtime.GOOS == "darwin" {
		tempDir, _ = filepath.EvalSymlinks(tempDir)
	}

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

		var out string

		// there is no pwd for windows, and getting it requires using a shell builtin
		if runtime.GOOS == "windows" {
			out, err = sh.RunAndCapture("cmd", "/c", "echo", "%cd%")
			if err != nil {
				t.Fatal(err)
			}
		} else {
			out, err = sh.RunAndCapture("pwd")
			if err != nil {
				t.Fatal(err)
			}
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

func TestLockFileRetriesAndTimesOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Flakey on windows")
	}

	dir, err := ioutil.TempDir("", "shelltest")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	sh := newShellForTest(t)
	sh.Logger = shell.DiscardLogger

	lockPath := filepath.Join(dir, "my.lock")

	// acquire a lock in another process
	cmd, err := acquireLockInOtherProcess(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	defer cmd.Process.Kill()

	// acquire lock
	_, err = sh.LockFile(lockPath, time.Second*2)
	if err != context.DeadlineExceeded {
		t.Fatalf("Expected DeadlineExceeded error, got %v", err)
	}
}

func acquireLockInOtherProcess(lockfile string) (*exec.Cmd, error) {
	cmd := exec.Command(os.Args[0], "-test.run=TestAcquiringLockHelperProcess", "--", lockfile)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1"}

	err := cmd.Start()
	if err != nil {
		return cmd, err
	}

	// wait for the above process to get a lock
	for {
		if _, err = os.Stat(lockfile); os.IsNotExist(err) {
			time.Sleep(time.Millisecond * 10)
			continue
		}
		break
	}

	return cmd, nil
}

// TestAcquiringLockHelperProcess isn't a real test. It's used as a helper process
func TestAcquiringLockHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	fileName := os.Args[len(os.Args)-1]
	sh := newShellForTest(t)

	log.Printf("Locking %s", fileName)
	if _, err := sh.LockFile(fileName, time.Second*10); err != nil {
		os.Exit(1)
	}

	log.Printf("Acquired lock %s", fileName)
	c := make(chan struct{})
	<-c
}

func newShellForTest(t *testing.T) *shell.Shell {
	sh, err := shell.New()
	if err != nil {
		t.Fatal(err)
	}
	sh.Logger = shell.DiscardLogger
	return sh
}

func TestRunWithoutPrompt(t *testing.T) {
	sh, err := shell.New()
	if err != nil {
		t.Fatal(err)
	}
	out := bytes.NewBufferString("")
	sh.Writer = out

	err = sh.RunWithoutPrompt("echo", "hi")
	assert.NoError(t, err)
	assert.Equal(t, "hi\n", out.String())

	out.Reset()
	err = sh.RunWithoutPrompt("asdasdasdasdzxczxczxzxc")
	assert.Error(t, err)
}

func TestRunWithoutPromptWithContext(t *testing.T) {
	sh, err := shell.New()
	ctx := context.Background()
	if err != nil {
		t.Fatal(err)
	}
	out := bytes.NewBufferString("")
	sh.Writer = out

	err = sh.RunWithoutPromptWithContext(ctx, "echo", "hi")
	assert.NoError(t, err)
	assert.Equal(t, "hi\n", out.String())

	out.Reset()
	err = sh.RunWithoutPromptWithContext(ctx, "asdasdasdasdzxczxczxzxc")
	assert.Error(t, err)
}
