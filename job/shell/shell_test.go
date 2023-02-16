package shell_test

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/experiments"
	"github.com/buildkite/agent/v3/job/shell"
	"github.com/buildkite/bintest/v3"
	"github.com/google/go-cmp/cmp"
)

func TestRunAndCaptureWithTTY(t *testing.T) {
	sshKeygen, err := bintest.CompileProxy("ssh-keygen")
	if err != nil {
		t.Fatalf("bintest.CompileProxy(ssh-keygen) error = %v", err)
	}
	defer sshKeygen.Close()

	sh := newShellForTest(t)
	sh.PTY = true

	go func() {
		call := <-sshKeygen.Ch
		fmt.Fprintln(call.Stdout, "Llama party! 🎉")
		call.Exit(0)
	}()

	got, err := sh.RunAndCapture(context.Background(), sshKeygen.Path, "-f", "my_hosts", "-F", "llamas.com")
	if err != nil {
		t.Errorf(`sh.RunAndCapture(ssh-keygen, "-f", "my_hosts", "-F", "llamas.com") error = %v`, err)
	}

	if want := "Llama party! 🎉"; got != want {
		t.Errorf(`sh.RunAndCapture(ssh-keygen, "-f", "my_hosts", "-F", "llamas.com") output = %q, want %q`, got, want)
	}
}

func TestRunAndCaptureWithExitCode(t *testing.T) {
	sshKeygen, err := bintest.CompileProxy("ssh-keygen")
	if err != nil {
		t.Fatalf("bintest.CompileProxy(ssh-keygen) error = %v", err)
	}
	defer sshKeygen.Close()

	sh := newShellForTest(t)

	go func() {
		call := <-sshKeygen.Ch
		fmt.Fprintln(call.Stdout, "Llama drama! 🚨")
		call.Exit(24)
	}()

	_, err = sh.RunAndCapture(context.Background(), sshKeygen.Path)
	if err == nil {
		t.Errorf("sh.RunAndCapture(ssh-keygen) error = %v, want non-nil error", err)
	}

	if got, want := shell.GetExitCode(err), 24; got != want {
		t.Errorf("shell.GetExitCode(%v) = %d, want %d", err, got, want)
	}
}

func TestRun(t *testing.T) {
	sshKeygen, err := bintest.CompileProxy("ssh-keygen")
	if err != nil {
		t.Fatalf("bintest.CompileProxy(ssh-keygen) error = %v", err)
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

	if err := sh.Run(context.Background(), sshKeygen.Path, "-f", "my_hosts", "-F", "llamas.com"); err != nil {
		t.Errorf(`sh.Run(ssh-keygen, "-f", "my_hosts", "-F", "llamas.com") error = %v`, err)
	}

	promptPrefix := "$"
	if runtime.GOOS == "windows" {
		promptPrefix = ">"
	}

	want := promptPrefix + " " + sshKeygen.Path + " -f my_hosts -F llamas.com\nLlama party! 🎉\n"
	if diff := cmp.Diff(out.String(), want); diff != "" {
		t.Fatalf("sh.Writer diff (-got +want):\n%s", diff)
	}
}

func TestRunWithStdin(t *testing.T) {
	out := &bytes.Buffer{}
	sh := newShellForTest(t)
	sh.Writer = out

	if err := sh.WithStdin(strings.NewReader("hello stdin")).Run(context.Background(), "tr", "hs", "HS"); err != nil {
		t.Fatalf(`sh.WithStdin("hello stdin").Run("tr", "hs", "HS") error = %v`, err)
	}
	if got, want := out.String(), "Hello Stdin"; want != got {
		t.Errorf(`sh.WithStdin("hello stdin").Run("tr", "hs", "HS") output = %q, want %q`, got, want)
	}
}

func TestContextCancelTerminates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Not supported in windows")
	}

	sleepCmd, err := bintest.CompileProxy("sleep")
	if err != nil {
		t.Fatalf("bintest.CompileProxy(sleep) error = %v", err)
	}
	defer sleepCmd.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sh, err := shell.New()
	if err != nil {
		t.Fatalf("shell.New() error = %v", err)
	}

	sh.Logger = shell.DiscardLogger

	go func() {
		call := <-sleepCmd.Ch
		time.Sleep(time.Second * 60)
		call.Exit(0)
	}()

	cancel()

	if err := sh.Run(ctx, sleepCmd.Path); !shell.IsExitSignaled(err) {
		t.Errorf("sh.Run(ctx, sleep) error = %v, want shell.IsExitSignaled(err) = true", err)
	}
}

func TestInterrupt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Not supported in windows")
	}

	sleepCmd, err := bintest.CompileProxy("sleep")
	if err != nil {
		t.Fatalf("bintest.CompileProxy(sleep) error = %v", err)
	}
	defer sleepCmd.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sh, err := shell.New()
	if err != nil {
		t.Fatalf("shell.New() error = %v", err)
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

	if err := sh.Run(ctx, sleepCmd.Path); err == nil {
		t.Errorf("sh.Run(ctx, sleep) = %v, want non-nil error", err)
	}
}

func TestDefaultWorkingDirFromSystem(t *testing.T) {
	sh, err := shell.New()
	if err != nil {
		t.Fatalf("shell.New() error = %v", err)
	}

	want, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v", err)
	}
	if got := sh.Getwd(); got != want {
		t.Fatalf("sh.Getwd() = %q, want %q", got, want)
	}
}

func TestWorkingDir(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "shelltest")
	if err != nil {
		t.Fatalf(`os.MkdirTemp("", "shelltest") error = %v`, err)
	}
	defer os.RemoveAll(tempDir)

	// macos has a symlinked temp dir
	if runtime.GOOS == "darwin" {
		td, err := filepath.EvalSymlinks(tempDir)
		if err != nil {
			t.Fatalf("filepath.EvalSymlinks(tempDir) error = %v", err)
		}
		tempDir = td
	}

	dirs := []string{tempDir, "my", "test", "dirs"}

	if err := os.MkdirAll(filepath.Join(dirs...), 0700); err != nil {
		t.Fatalf("os.MkdirAll(dirs, 0700) = %v", err)
	}

	currentWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v", err)
	}

	sh, err := shell.New()
	if err != nil {
		t.Fatalf("shell.New() error = %v", err)
	}

	sh.Logger = shell.DiscardLogger

	for idx := range dirs {
		dir := filepath.Join(dirs[:idx+1]...)

		if err := sh.Chdir(dir); err != nil {
			t.Fatalf("sh.Chdir(%q) = %v", dir, err)
		}

		if got, want := sh.Getwd(), dir; got != want {
			t.Fatalf("sh.Getwd() = %q, want %q", got, want)
		}

		var pwd string

		// there is no pwd for windows, and getting it requires using a shell builtin
		if runtime.GOOS == "windows" {
			out, err := sh.RunAndCapture(context.Background(), "cmd", "/c", "echo", "%cd%")
			if err != nil {
				t.Fatalf("sh.RunAndCapture(cmd /c echo %%cd%%) error = %v", err)
			}
			pwd = out
		} else {
			out, err := sh.RunAndCapture(context.Background(), "pwd")
			if err != nil {
				t.Fatalf("sh.RunAndCapture(pwd) error = %v", err)
			}
			pwd = out
		}

		if got, want := pwd, dir; got != want {
			t.Fatalf("sh.RunAndCapture(pwd or equivalent) = %q, want %q", got, want)
		}
	}

	afterWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v", err)
	}
	if got, want := afterWd, currentWd; got != want {
		// Expect working dir to be the same as before shell commands ran.
		t.Fatalf("os.Getwd() = %q, want %q", got, want)
	}
}

func TestLockFileRetriesAndTimesOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Flakey on windows")
	}

	dir, err := os.MkdirTemp("", "shelltest")
	if err != nil {
		t.Fatalf(`os.MkdirTemp("", "shelltest") error = %v`, err)
	}
	defer os.RemoveAll(dir)

	sh := newShellForTest(t)
	sh.Logger = shell.DiscardLogger

	lockPath := filepath.Join(dir, "my.lock")

	// acquire a lock in another process
	cmd, err := acquireLockInOtherProcess(lockPath)
	if err != nil {
		t.Errorf("acquireLockInOtherProcess(%q) error = %v", lockPath, err)
	}
	defer cmd.Process.Kill()

	timeout := time.Second * 2
	if _, err := sh.LockFile(context.Background(), lockPath, timeout); err != context.DeadlineExceeded {
		t.Errorf("sh.LockFile(%q, %v) error = %v, want context.DeadlineExceeded", lockPath, timeout, err)
	}
}

func TestFlockRetriesAndTimesOut(t *testing.T) {
	experiments.Enable(experiments.FlockFileLocks)
	defer experiments.Disable(experiments.FlockFileLocks)

	TestLockFileRetriesAndTimesOut(t)
}

func acquireLockInOtherProcess(lockfile string) (*exec.Cmd, error) {
	flockExperimentEnabled := false
	expectedLockPath := lockfile
	if experiments.IsEnabled(experiments.FlockFileLocks) {
		flockExperimentEnabled = true
		expectedLockPath = lockfile + "f" // flock-locked files are created with the suffix 'f'
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestAcquiringLockHelperProcess", "--", lockfile)
	cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "FLOCK_EXPERIMENT_ENABLED=" + strconv.FormatBool(flockExperimentEnabled)}

	err := cmd.Start()
	if err != nil {
		return cmd, err
	}

	// wait for the above process to get a lock
	for {
		if _, err = os.Stat(expectedLockPath); os.IsNotExist(err) {
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

	if os.Getenv("FLOCK_EXPERIMENT_ENABLED") == "true" {
		experiments.Enable(experiments.FlockFileLocks)
	} else {
		experiments.Disable(experiments.FlockFileLocks)
	}

	fileName := os.Args[len(os.Args)-1]
	sh := newShellForTest(t)

	log.Printf("Locking %s", fileName)
	if _, err := sh.LockFile(context.Background(), fileName, time.Second*10); err != nil {
		os.Exit(1)
	}

	log.Printf("Acquired lock %s", fileName)
	c := make(chan struct{})
	<-c
}

func newShellForTest(t *testing.T) *shell.Shell {
	sh, err := shell.New()
	if err != nil {
		t.Fatalf("shell.New() error = %v", err)
	}
	sh.Logger = shell.DiscardLogger
	return sh
}

func TestRunWithoutPrompt(t *testing.T) {
	sh, err := shell.New()
	if err != nil {
		t.Fatalf("shell.New() error = %v", err)
	}
	out := &bytes.Buffer{}
	sh.Writer = out

	if err := sh.RunWithoutPrompt(context.Background(), "echo", "hi"); err != nil {
		t.Fatalf("sh.RunWithoutPrompt(echo hi) = %v", err)
	}
	if got, want := out.String(), "hi\n"; got != want {
		t.Errorf("sh.RunWithoutPrompt(echo hi) output = %q, want %q", got, want)
	}

	out.Reset()
	if err := sh.RunWithoutPrompt(context.Background(), "asdasdasdasdzxczxczxzxc"); err == nil {
		t.Errorf("sh.RunWithoutPrompt(asdasdasdasdzxczxczxzxc) = %v, want non-nil error", err)
	}
}

func TestRound(t *testing.T) {
	tests := []struct {
		in      time.Duration
		want    time.Duration
		wantStr string
	}{
		{3 * time.Nanosecond, 3 * time.Nanosecond, "3ns"},
		{32 * time.Nanosecond, 32 * time.Nanosecond, "32ns"},
		{321 * time.Nanosecond, 321 * time.Nanosecond, "321ns"},
		{4321 * time.Nanosecond, 4321 * time.Nanosecond, "4.321µs"},
		{54321 * time.Nanosecond, 54321 * time.Nanosecond, "54.321µs"},
		{654321 * time.Nanosecond, 654320 * time.Nanosecond, "654.32µs"},
		{7654321 * time.Nanosecond, 7654300 * time.Nanosecond, "7.6543ms"},
		{87654321 * time.Nanosecond, 87654000 * time.Nanosecond, "87.654ms"},
		{987654321 * time.Nanosecond, 987650000 * time.Nanosecond, "987.65ms"},
		{1987654321 * time.Nanosecond, 1987700000 * time.Nanosecond, "1.9877s"},
		{21987654321 * time.Nanosecond, 21988000000 * time.Nanosecond, "21.988s"},
		{321987654321 * time.Nanosecond, 321990000000 * time.Nanosecond, "5m21.99s"},
		{4321987654321 * time.Nanosecond, 4320000000000 * time.Nanosecond, "1h12m0s"},
		{54321987654321 * time.Nanosecond, 54320000000000 * time.Nanosecond, "15h5m20s"},
	}

	for _, tt := range tests {
		got := shell.Round(tt.in)
		if got != tt.want {
			t.Errorf("shell.Round(%v): got %v, want %v", tt.in, got, tt.want)
		}
		if got.String() != tt.wantStr {
			t.Errorf("shell.Round(%v): got %q, want %v", tt.in, got.String(), tt.wantStr)
		}
	}
}
