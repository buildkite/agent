package shell_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/process"
	"github.com/buildkite/agent/v3/internal/replacer"
	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/bintest/v3"
	"github.com/google/go-cmp/cmp"
	"gotest.tools/v3/assert"
)

func TestRunAndCaptureWithTTY(t *testing.T) {
	t.Parallel()

	sshKeygen, err := bintest.CompileProxy("ssh-keygen")
	if err != nil {
		t.Fatalf("bintest.CompileProxy(ssh-keygen) error = %v", err)
	}
	defer func() {
		if err := sshKeygen.Close(); err != nil {
			t.Errorf("sshKeygen.Close() = %v", err)
		}
	}()

	// WithPTY(true) should be overriden by RunAndCapture.
	sh := newShellForTest(t, shell.WithPTY(true))

	go func() {
		call := <-sshKeygen.Ch
		_, _ = fmt.Fprintln(call.Stdout, "Llama party! 🎉")
		call.Exit(0)
	}()

	cmd := sh.Command(sshKeygen.Path, "-f", "my_hosts", "-F", "llamas.com")
	got, err := cmd.RunAndCaptureStdout(t.Context())
	if err != nil {
		t.Errorf(`sh.Command(ssh-keygen, "-f", "my_hosts", "-F", "llamas.com").RunAndCaptureStdout(t.Context()) error = %v`, err)
	}

	if want := "Llama party! 🎉"; got != want {
		t.Errorf(`sh.Command(ssh-keygen, "-f", "my_hosts", "-F", "llamas.com").RunAndCaptureStdout(t.Context()) output = %q, want %q`, got, want)
	}
}

func TestRunAndCaptureWithExitCode(t *testing.T) {
	t.Parallel()

	sshKeygen, err := bintest.CompileProxy("ssh-keygen")
	if err != nil {
		t.Fatalf("bintest.CompileProxy(ssh-keygen) error = %v", err)
	}
	defer func() {
		if err := sshKeygen.Close(); err != nil {
			t.Errorf("sshKeygen.Close() = %v", err)
		}
	}()

	sh := newShellForTest(t, shell.WithPTY(false))

	go func() {
		call := <-sshKeygen.Ch
		_, _ = fmt.Fprintln(call.Stdout, "Llama drama! 🚨")
		call.Exit(24)
	}()

	_, err = sh.Command(sshKeygen.Path).RunAndCaptureStdout(t.Context())
	if err == nil {
		t.Errorf("sh.Command(ssh-keygen).RunAndCaptureStdout(t.Context()) error = %v, want non-nil error", err)
	}

	if got, want := shell.ExitCode(err), 24; got != want {
		t.Errorf("shell.GetExitCode(%v) = %d, want %d", err, got, want)
	}
}

func TestRun(t *testing.T) {
	t.Parallel()

	sshKeygen, err := bintest.CompileProxy("ssh-keygen")
	if err != nil {
		t.Fatalf("bintest.CompileProxy(ssh-keygen) error = %v", err)
	}
	defer func() {
		if err := sshKeygen.Close(); err != nil {
			t.Errorf("sshKeygen.Close() = %v", err)
		}
	}()

	out := &bytes.Buffer{}

	sh := newShellForTest(t,
		shell.WithLogger(shell.NewWriterLogger(out, false, nil)),
		shell.WithStdout(out),
		shell.WithPTY(false),
	)

	go func() {
		call := <-sshKeygen.Ch
		_, _ = fmt.Fprintln(call.Stdout, "Llama party! 🎉")
		call.Exit(0)
	}()

	cmd := sh.Command(sshKeygen.Path, "-f", "my_hosts", "-F", "llamas.com")
	if err := cmd.Run(t.Context()); err != nil {
		t.Errorf(`sh.Command(ssh-keygen, "-f", "my_hosts", "-F", "llamas.com").Run(t.Context()) error = %v`, err)
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
	t.Parallel()

	out := &bytes.Buffer{}
	sh := newShellForTest(t, shell.WithStdout(out), shell.WithPTY(false))
	cmd := sh.CloneWithStdin(strings.NewReader("hello stdin")).Command("tr", "hs", "HS")
	if err := cmd.Run(t.Context()); err != nil {
		t.Fatalf(`sh.WithStdin("hello stdin").Run("tr", "hs", "HS") error = %v`, err)
	}
	if got, want := out.String(), "Hello Stdin"; want != got {
		t.Errorf(`sh.WithStdin("hello stdin").Run("tr", "hs", "HS") output = %q, want %q`, got, want)
	}
}

func TestContextCancelInterrupts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		cmdWindows        string
		newConsole        bool
		interruptSignal   process.Signal
		wantSignal        syscall.Signal
		wantSignalWindows syscall.Signal
	}{
		{
			name: "default",
			// The usual timeout command should obey Ctrl-Break.
			cmdWindows: ".\\fixtures\\sleep60.bat",
			// The default interrupt signal is SIGTERM.
			wantSignal:        syscall.SIGTERM,
			wantSignalWindows: -1,
		},
		{
			name: "SIGINT",
			// SIGINT does nothing different on Windows.
			cmdWindows:        ".\\fixtures\\sleep60.bat",
			interruptSignal:   process.SIGINT,
			wantSignal:        syscall.SIGINT,
			wantSignalWindows: -1,
		},
		{
			name: "SIGKILL",
			// SIGKILL should bypass the Ctrl-Break handler in the process.
			cmdWindows:        ".\\fixtures\\sleep60nobreak.bat",
			interruptSignal:   process.SIGKILL,
			wantSignal:        syscall.SIGKILL,
			wantSignalWindows: -1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			opts := []shell.NewShellOpt{
				shell.WithSignalGracePeriod(10 * time.Second),
			}
			if test.interruptSignal != 0 {
				opts = append(opts, shell.WithInterruptSignal(test.interruptSignal))
			}

			sh, err := shell.New(opts...)
			if err != nil {
				t.Fatalf("shell.New(%v) error = %v", opts, err)
			}

			sh.Logger = shell.DiscardLogger

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			// Wait for the process to start before cancelling the context.
			// This is more reliable than time.Sleep.
			started := make(chan struct{})

			go func() {
				select {
				case <-time.After(1 * time.Minute):
					t.Error("timeout waiting for process to start")
				case <-started:
					// Now cancel the context, which should cause the interrupt.
					cancel()
				}
			}()

			cmd := "fixtures/sleep60.sh"
			if runtime.GOOS == "windows" {
				cmd = test.cmdWindows
			}

			// The process returns an exit error, except on Windows. 🙄
			wantSignaled := runtime.GOOS != "windows"

			err = sh.Command(cmd).Run(ctx, shell.WithStarted(started))
			if got, want := shell.IsExitSignaled(err), wantSignaled; got != want {
				t.Errorf("sh.Command(%q).Run(ctx) = %v, want shell.IsExitSignaled(err) = %t", cmd, err, want)
			}

			status, err := sh.WaitStatus()
			if err != nil {
				t.Errorf("sh.WaitStatus() error = %v", err)
			}
			wantSignal := test.wantSignal
			if runtime.GOOS == "windows" {
				wantSignal = test.wantSignalWindows
			}
			if got, want := status.Signal(), wantSignal; got != want {
				t.Errorf("sh.WaitStatus().Signal() = %v, want %v", got, want)
			}
		})
	}
}

func TestInterrupt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		cmdWindows        string
		interruptSignal   process.Signal
		wantSignal        syscall.Signal
		wantSignalWindows syscall.Signal
	}{
		{
			name: "default",
			// The usual timeout command should obey Ctrl-Break.
			cmdWindows:        ".\\fixtures\\sleep60.bat",
			wantSignal:        syscall.SIGTERM,
			wantSignalWindows: -1,
		},
		{
			name:              "SIGINT",
			cmdWindows:        ".\\fixtures\\sleep60.bat",
			interruptSignal:   process.SIGINT,
			wantSignal:        syscall.SIGINT,
			wantSignalWindows: -1,
		},
		{
			name:              "SIGKILL",
			cmdWindows:        ".\\fixtures\\sleep60nobreak.bat",
			interruptSignal:   process.SIGKILL,
			wantSignal:        syscall.SIGKILL,
			wantSignalWindows: -1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			opts := []shell.NewShellOpt{
				shell.WithSignalGracePeriod(10 * time.Second),
			}
			if test.interruptSignal != 0 {
				opts = append(opts, shell.WithInterruptSignal(test.interruptSignal))
			}
			sh, err := shell.New(opts...)
			if err != nil {
				t.Fatalf("shell.New(%v) error = %v", opts, err)
			}

			sh.Logger = shell.DiscardLogger

			// Wait for the process to start before interrupting.
			// This is more reliable than time.Sleep.
			started := make(chan struct{})

			go func() {
				select {
				case <-time.After(1 * time.Minute):
					t.Error("timeout waiting for process to start")
				case <-started:
					// Now interrupt the process.
					if err := sh.Interrupt(); err != nil {
						t.Errorf("sh.Interrupt() = %v", err)
					}
				}
			}()

			cmd := "fixtures/sleep60.sh"
			if runtime.GOOS == "windows" {
				cmd = test.cmdWindows
			}

			// The process returns an exit error, except on Windows. 🙄
			wantSignaled := runtime.GOOS != "windows"

			err = sh.Command(cmd).Run(t.Context(), shell.WithStarted(started))
			if got, want := shell.IsExitSignaled(err), wantSignaled; got != want {
				t.Errorf("sh.Command(%q).Run(t.Context()) = %v, want shell.IsExitSignaled(err) = %t", cmd, err, want)
			}

			status, err := sh.WaitStatus()
			if err != nil {
				t.Errorf("sh.WaitStatus() error = %v", err)
			}
			wantSignal := test.wantSignal
			if runtime.GOOS == "windows" {
				wantSignal = test.wantSignalWindows
			}
			if got, want := status.Signal(), wantSignal; got != want {
				t.Errorf("sh.WaitStatus().Signal() = %v, want %v", got, want)
			}
		})
	}
}

func TestDefaultWorkingDirFromSystem(t *testing.T) {
	t.Parallel()

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
	t.Parallel()
	ctx := context.Background()

	tempDir, err := os.MkdirTemp("", "shelltest")
	if err != nil {
		t.Fatalf(`os.MkdirTemp("", "shelltest") error = %v`, err)
	}
	t.Cleanup(func() {
		os.RemoveAll(tempDir) //nolint:errcheck // Best-effort cleanup.
	})

	// macos has a symlinked temp dir
	if runtime.GOOS == "darwin" {
		td, err := filepath.EvalSymlinks(tempDir)
		if err != nil {
			t.Fatalf("filepath.EvalSymlinks(tempDir) error = %v", err)
		}
		tempDir = td
	}

	dirs := []string{tempDir, "my", "test", "dirs"}

	if err := os.MkdirAll(filepath.Join(dirs...), 0o700); err != nil {
		t.Fatalf("os.MkdirAll(dirs, 0o700) = %v", err)
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
			out, err := sh.Command("cmd", "/c", "echo", "%cd%").RunAndCaptureStdout(ctx)
			if err != nil {
				t.Fatalf("sh.RunAndCapture(cmd /c echo %%cd%%) error = %v", err)
			}
			pwd = out
		} else {
			out, err := sh.Command("pwd").RunAndCaptureStdout(ctx)
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
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Flakey on windows")
	}

	dir, err := os.MkdirTemp("", "TestLockFileRetriesAndTimesOut")
	if err != nil {
		t.Fatalf(`os.MkdirTemp("", "TestLockFileRetriesAndTimesOut") error = %v`, err)
	}
	t.Cleanup(func() {
		os.RemoveAll(dir) //nolint:errcheck // Best-effort cleanup.
	})

	sh := newShellForTest(t,
		shell.WithStdout(io.Discard),
		shell.WithPTY(false),
	)

	lockPath := filepath.Join(dir, "my.lock")

	cmd := acquireLockInOtherProcess(t, lockPath)
	defer func() { assert.NilError(t, cmd.Process.Kill()) }()

	ctx, canc := context.WithTimeout(t.Context(), 2*time.Second)
	defer canc()

	lock, err := sh.LockFile(ctx, lockPath)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, lock, nil)
}

func acquireLockInOtherProcess(t *testing.T, lockfile string) *exec.Cmd {
	t.Helper()

	t.Logf("acquiring lock in other process: %s", lockfile)

	done := make(chan struct{})
	search := replacer.New(os.Stderr, []string{"🔒 Acquired lock"}, func(b []byte) []byte {
		t.Logf("✅ Acquired lock in other process!")
		close(done)
		return b
	})

	cmd := exec.Command(os.Args[0], "--", lockfile)
	cmd.Env = []string{"TEST_MAIN_WANT_HELPER_PROCESS=1"}
	cmd.Stdout = os.Stdout
	cmd.Stderr = search

	err := cmd.Start()
	assert.NilError(t, err)

	// wait for the above process to get a lock
	// This used to be a stat loop, checking for the existence of the lock file.
	// But lock files are a two-step process (open, then flock), so a stat loop
	// doesn't end when the child process has the lock, only when the file
	// exists, so occasionally the unit test would successfully grab the lock.
	<-done

	return cmd
}

func newShellForTest(t *testing.T, opts ...shell.NewShellOpt) *shell.Shell {
	t.Helper()
	sh, err := shell.New(opts...)
	if err != nil {
		t.Fatalf("shell.New() error = %v", err)
	}
	return sh
}

func TestRunWithOlfactor(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name           string
		smellsToSniff  []string
		command        []string
		output         string
		smellsInOutput []string
	}{
		{
			name:           "smells_stdout",
			smellsToSniff:  []string{"hi"},
			command:        []string{"bash", "-ec", "echo hi; false"},
			output:         "hi\n",
			smellsInOutput: []string{"hi"},
		},
		{
			name:           "smells_stderr",
			smellsToSniff:  []string{"hi"},
			command:        []string{"bash", "-ec", "echo hi >&2; false"},
			output:         "hi\n",
			smellsInOutput: []string{"hi"},
		},
		{
			name:           "smells_infix",
			smellsToSniff:  []string{"hi"},
			command:        []string{"bash", "-ec", "echo lorem ipsum; echo hi; echo bye; false"},
			output:         "lorem ipsum\nhi\nbye\n",
			smellsInOutput: []string{"hi"},
		},
		{
			name:           "smells_infix_inline",
			smellsToSniff:  []string{"hi"},
			command:        []string{"bash", "-ec", "echo lorem hipsum; echo bye; false"},
			output:         "lorem hipsum\nbye\n",
			smellsInOutput: []string{"hi"},
		},
		{
			name:           "smells_partial",
			smellsToSniff:  []string{"ar"},
			command:        []string{"bash", "-ec", "echo hi, how are you; false"},
			output:         "hi, how are you\n",
			smellsInOutput: []string{"ar"},
		},
		{
			name:           "no_smell",
			smellsToSniff:  []string{"hi"},
			command:        []string{"bash", "-ec", "echo bye, how were you; false"},
			output:         "bye, how were you\n",
			smellsInOutput: []string{},
		},
		{
			name:           "multiple_smells",
			smellsToSniff:  []string{"bye", "how"},
			command:        []string{"bash", "-ec", "echo bye, how were you; false"},
			output:         "bye, how were you\n",
			smellsInOutput: []string{"bye", "how"},
		},
		{
			name:           "smells_when_exit_0",
			smellsToSniff:  []string{"how"},
			command:        []string{"bash", "-ec", "echo hi, how are you?"},
			output:         "hi, how are you?\n",
			smellsInOutput: []string{"how"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			out := &bytes.Buffer{}
			sh, err := shell.New(shell.WithStdout(out))
			assert.NilError(t, err)

			smelled := make(map[string]bool)
			for _, s := range test.smellsToSniff {
				smelled[s] = false
			}

			err = sh.Command(test.command[0], test.command[1:]...).Run(
				t.Context(),
				shell.WithStringSearch(smelled),
			)
			if eerr := new(exec.ExitError); !errors.As(err, &eerr) {
				assert.NilError(t, err)
			}

			if diff := cmp.Diff(out.String(), test.output); diff != "" {
				t.Errorf("stdout diff (-got +want):\n%s", diff)
			}

			for _, smell := range test.smellsInOutput {
				if smelt := smelled[smell]; !smelt {
					t.Errorf("smelled[%q] = %t, want true (smelt)", smell, smelt)
				}
				delete(smelled, smell)
			}
			for smell, smelt := range smelled {
				if smelt {
					t.Errorf("smelled[%q] = %t, want false (not smelt)", smell, smelt)
				}
			}
		})
	}
}

func TestRunWithoutPrompt(t *testing.T) {
	t.Parallel()

	out := &bytes.Buffer{}
	sh, err := shell.New(shell.WithStdout(out))
	if err != nil {
		t.Fatalf("shell.New() error = %v", err)
	}

	if err := sh.Command("echo", "hi").Run(t.Context(), shell.ShowPrompt(false)); err != nil {
		t.Fatalf(`sh.Command("echo", "hi").Run(ctx, shell.ShowPrompt(false)) = %v`, err)
	}
	if got, want := out.String(), "hi\n"; got != want {
		t.Errorf(`sh.Command("echo", "hi").Run(ctx, shell.ShowPrompt(false)) output = %q, want %q`, got, want)
	}

	out.Reset()
	if err := sh.Command("asdasdasdasdzxczxczxzxc").Run(t.Context(), shell.ShowPrompt(false)); err == nil {
		t.Errorf("sh.RunWithoutPrompt(asdasdasdasdzxczxczxzxc) = %v, want non-nil error", err)
	}
}

func TestRound(t *testing.T) {
	t.Parallel()

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
		t.Run(tt.wantStr, func(t *testing.T) {
			t.Parallel()
			got := shell.Round(tt.in)
			if got != tt.want {
				t.Errorf("shell.Round(%v): got %v, want %v", tt.in, got, tt.want)
			}
			if got.String() != tt.wantStr {
				t.Errorf("shell.Round(%v): got %q, want %v", tt.in, got.String(), tt.wantStr)
			}
		})
	}
}
