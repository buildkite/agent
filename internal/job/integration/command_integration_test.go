package integration

import (
	"runtime"
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
			code:    "command_failed",
			message: "The command `echo 'Running the test suite'; exit 3` exited with status 3.\n\nLast lines of output:\nRunning the test suite",
		},
		{
			name:    "signal",
			env:     []string{"BUILDKITE_COMMAND=kill -KILL $$"},
			code:    "command_failed",
			message: "The command `kill -KILL $$` was terminated by signal SIGKILL. The operating system may have killed it for using too much memory.",
		},
		{
			name: "missing command",
			env:  []string{"BUILDKITE_COMMAND="},
			code: "command_missing",
		},
		{
			name:    "command eval disabled",
			env:     []string{"BUILDKITE_COMMAND=echo hello", "BUILDKITE_COMMAND_EVAL=false"},
			code:    "command_eval_disabled",
			message: "this agent is not allowed to evaluate console commands; to allow this, re-run the agent without the `--no-command-eval` option or specify a script within your repository to run instead (such as scripts/test.sh). The step's command is `echo hello`.",
		},
		{
			name:    "command not run",
			env:     []string{"BUILDKITE_COMMAND=echo hello", `BUILDKITE_SHELL=/bin/bash "-c`},
			code:    "command_not_run",
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
