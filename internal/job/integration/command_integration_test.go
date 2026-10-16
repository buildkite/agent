package integration

import (
	"errors"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/job"
	"github.com/buildkite/bintest/v3"
)

func TestMultilineCommandRunUnderBatch(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "windows" {
		t.Skip("batch test only applies to Windows")
	}

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	setup := tester.MustMock(t, "Setup.cmd")
	build := tester.MustMock(t, "BuildProject.cmd")

	setup.Expect().Once()
	build.Expect().Once().AndCallFunc(func(c *bintest.Call) {
		if got, want := c.GetEnv("LLAMAS"), "COOL"; got != want {
			t.Errorf("c.GetEnv(LLAMAS) = %q, want %q", got, want)
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	env := []string{
		"BUILDKITE_COMMAND=Setup.cmd\nset LLAMAS=COOL\nBuildProject.cmd",
		`BUILDKITE_SHELL=C:\Windows\System32\CMD.exe /S /C`,
		`BUILDKITE_HOOKS_SHELL=C:\Program Files\PowerShell\7\pwsh.exe`,
	}

	tester.RunAndCheck(t, env...)
}

func TestSignalledCommandExitStatus(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("kill -TERM needs a POSIX shell")
	}

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	assertExitStatus := func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_COMMAND_EXIT_STATUS"), "143"; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_COMMAND_EXIT_STATUS) = %q, want %q", got, want)
		}
		c.Exit(0)
	}
	tester.ExpectGlobalHook("pre-exit").Once().AndCallFunc(assertExitStatus)
	tester.ExpectLocalHook("pre-exit").Once().AndCallFunc(assertExitStatus)

	// The command kills itself with SIGTERM. A signalled command has no exit
	// status of its own, so the bootstrap should exit 128+N (143) rather
	// than 255, which is how Go's -1 signalled sentinel survives os.Exit's
	// 8-bit truncation.
	err = tester.Run(t, `BUILDKITE_COMMAND=kill -TERM $$`)

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("tester.Run() error = %v, want *exec.ExitError", err)
	}
	if got, want := exitErr.ExitCode(), 143; got != want {
		t.Errorf("bootstrap exit code = %d, want %d", got, want)
	}

	tester.CheckMocks(t)
}

func TestCancelledCommandExitStatus(t *testing.T) {
	t.Parallel()

	// Same constraints as TestPreExitHooksFireAfterCancel.
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip()
	}

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	assertExitStatus := func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_COMMAND_EXIT_STATUS"), "143"; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_COMMAND_EXIT_STATUS) = %q, want %q", got, want)
		}
		c.Exit(0)
	}
	tester.ExpectGlobalHook("pre-exit").Once().AndCallFunc(assertExitStatus)
	tester.ExpectLocalHook("pre-exit").Once().AndCallFunc(assertExitStatus)

	commandStarted := make(chan struct{})
	commandFinished := make(chan struct{})
	tester.MustMock(t, "blocking-command").Expect().Once().AndCallFunc(func(*bintest.Call) {
		close(commandStarted)
		<-commandFinished
	})

	runDone := make(chan error, 1)
	go func() {
		runDone <- tester.Run(t, "BUILDKITE_COMMAND=blocking-command")
	}()

	select {
	case <-commandStarted:
	case err := <-runDone:
		close(commandFinished)
		t.Fatalf("tester.Run() finished before command started: %v", err)
	case <-time.After(30 * time.Second):
		close(commandFinished)
		_ = tester.Cancel()
		t.Fatal("timed out waiting for command to start")
	}

	if err := tester.Cancel(); err != nil {
		t.Errorf("tester.Cancel() = %v", err)
	}

	select {
	case err := <-runDone:
		// After cancellation the bootstrap re-signals itself with the
		// received signal, so the run ends with death by SIGINT rather than
		// a plain exit status.
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Errorf("tester.Run() error = %v, want *exec.ExitError", err)
			break
		}
		ws, ok := exitErr.Sys().(syscall.WaitStatus)
		if !ok || !ws.Signaled() || ws.Signal() != syscall.SIGINT {
			t.Errorf("bootstrap wait status = %v, want signalled with SIGINT", exitErr)
		}
	case <-time.After(30 * time.Second):
		close(commandFinished)
		t.Fatal("timed out waiting for command to finish after cancel")
	}
	close(commandFinished)

	tester.CheckMocks(t)
}

func TestPreExitHooksRunsAfterCommandFails(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MockAgent(t)
	agent.
		Expect("meta-data", "exists", job.CommitMetadataKey).
		AndExitWith(0)

	preExitFunc := func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_COMMAND_EXIT_STATUS"), "1"; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_COMMAND_EXIT_STATUS) = %q, want %q", got, want)
		}
		c.Exit(0)
	}

	tester.ExpectGlobalHook("pre-exit").Once().AndCallFunc(preExitFunc)
	tester.ExpectLocalHook("pre-exit").Once().AndCallFunc(preExitFunc)

	if err := tester.Run(t, "BUILDKITE_COMMAND=false"); err == nil {
		t.Fatalf("tester.Run(t, BUILDKITE_COMMAND=false) = %v, want non-nil error", err)
	}

	tester.CheckMocks(t)
}

func TestCommandFailuresAreCaptured(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("commands use POSIX shell syntax")
	}

	for _, test := range []struct {
		name    string
		env     []string
		code    string
		message string
	}{
		{
			name:    "exit status",
			env:     []string{"BUILDKITE_COMMAND=echo 'Running the test suite'; exit 3"},
			code:    "buildkite.command_failed",
			message: "The command `echo 'Running the test suite'; exit 3` exited with status 3.\n\nLast lines of output:\nRunning the test suite",
		},
		{
			name:    "signal",
			env:     []string{"BUILDKITE_COMMAND=kill -KILL $$"},
			code:    "buildkite.command_failed",
			message: "The command `kill -KILL $$` was terminated by signal SIGKILL. The operating system may have killed it for using too much memory.",
		},
		{
			name: "missing command",
			env:  []string{"BUILDKITE_COMMAND="},
			code: "buildkite.command_not_configured",
		},
		{
			name:    "command eval disabled",
			env:     []string{"BUILDKITE_COMMAND=echo hello", "BUILDKITE_COMMAND_EVAL=false"},
			code:    "buildkite.command_eval_rejected",
			message: "this agent is not allowed to evaluate console commands; to allow this, re-run the agent without the `--no-command-eval` option or specify a script within your repository to run instead (such as scripts/test.sh). The step's command is `echo hello`.",
		},
		{
			// The hook did not exist when the agent chose what to run.
			name:    "command that creates a command hook",
			env:     []string{"BUILDKITE_COMMAND=mkdir -p .buildkite/hooks && touch .buildkite/hooks/command && exit 4"},
			code:    "buildkite.command_failed",
			message: "The command `mkdir -p .buildkite/hooks && touch .buildkite/hooks/command && exit 4` exited with status 4.",
		},
		{
			name:    "command not run",
			env:     []string{"BUILDKITE_COMMAND=echo hello", `BUILDKITE_SHELL=/bin/bash "-c`},
			code:    "buildkite.command_start_failed",
			message: "The command `echo hello` could not be run: failed to split shell (\"/bin/bash \\\"-c\") into tokens: expected closing quote \" at offset 12, got EOF.",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			tester, err := NewExecutorTester(mainCtx)
			if err != nil {
				t.Fatalf("NewExecutorTester() error = %v", err)
			}
			defer tester.Close()
			agentAPI := newJobErrorsAPI(t, nil)

			if err := tester.Run(t, append(agentAPI.env(), test.env...)...); err == nil {
				t.Fatalf("tester.Run() = nil, want command failure")
			}

			report := agentAPI.report(t, test.code)
			if test.message != "" && report.Message != test.message {
				t.Errorf("message = %q, want %q", report.Message, test.message)
			}
		})
	}
}

func TestRefusedCommandHookIsCapturedOnce(t *testing.T) {
	t.Parallel()
	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()
	agentAPI := newJobErrorsAPI(t, nil)

	tester.ExpectLocalHook("command").NotCalled()
	if err := tester.Run(t, append(agentAPI.env(), "BUILDKITE_NO_LOCAL_HOOKS=true")...); err == nil {
		t.Fatalf("tester.Run() = nil, want local hook refusal")
	}
	agentAPI.report(t, "buildkite.local_hook_rejected")
	if codes := agentAPI.codes(); len(codes) != 1 {
		t.Errorf("captured codes = %v, want only buildkite.local_hook_rejected", codes)
	}
}

func TestCancelledCommandIsNotCaptured(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("cancellation uses SIGINT")
	}

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()
	agentAPI := newJobErrorsAPI(t, nil)

	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	tester.MustMock(t, "blocking-command").Expect().Once().AndCallFunc(func(c *bintest.Call) {
		close(started)
		<-release
		c.Exit(1)
	})

	runDone := make(chan error, 1)
	go func() { runDone <- tester.Run(t, append(agentAPI.env(), "BUILDKITE_COMMAND=blocking-command")...) }()
	select {
	case <-started:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for command to start")
	}
	if err := tester.Cancel(); err != nil {
		t.Fatalf("tester.Cancel() = %v", err)
	}
	select {
	case <-runDone:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for cancelled job to finish")
	}

	if codes := agentAPI.codes(); len(codes) != 0 {
		t.Errorf("captured codes = %v, want none for a cancelled job", codes)
	}
}
