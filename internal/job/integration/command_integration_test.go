package integration

import (
	"runtime"
	"testing"

	"github.com/buildkite/agent/v3/internal/job"
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
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

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
	}

	tester.RunAndCheck(t, env...)
}

func TestPreExitHooksRunsAfterCommandFails(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

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
