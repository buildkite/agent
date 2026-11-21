package integration

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/buildkite/agent/v4/internal/job"
	"github.com/buildkite/bintest/v3"
)

func TestStepReuseRestoresBeforeAndSavesAfterCommand(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer func() {
		if err := tester.CloseErr(); err != nil {
			t.Errorf("tester.Close() = %v", err)
		}
	}()

	restored := false
	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", job.CommitMetadataKey).AndExitWith(0)
	agent.Expect("reuse", "restore").Once().AndCallFunc(func(c *bintest.Call) {
		restored = true
		c.Exit(0)
	})
	tester.MustMock(t, "my-command").Expect().Once().AndCallFunc(func(c *bintest.Call) {
		if !restored {
			t.Errorf("command ran before reuse restore")
		}
		c.Exit(0)
	})
	agent.Expect("reuse", "save", "--exit-status=0").Once().AndExitWith(0)

	tester.RunAndCheck(t, "BUILDKITE_STEP_REUSE=true", "BUILDKITE_COMMAND=my-command")
}

func TestStepReuseInputUnavailableSkipsCommand(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer func() {
		if err := tester.CloseErr(); err != nil {
			t.Errorf("tester.Close() = %v", err)
		}
	}()

	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", job.CommitMetadataKey).AndExitWith(0)
	agent.Expect("reuse", "restore").Once().AndExitWith(94)
	tester.MustMock(t, "my-command").Expect().NotCalled()

	err = tester.Run(t, "BUILDKITE_STEP_REUSE=true", "BUILDKITE_COMMAND=my-command")
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 94 {
		t.Fatalf("tester.Run() error = %v, want exit status 94", err)
	}
	tester.CheckMocks(t)
}

func TestStepReuseRestoreFailureContinues(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer func() {
		if err := tester.CloseErr(); err != nil {
			t.Errorf("tester.Close() = %v", err)
		}
	}()

	agent := tester.MockAgent(t)
	agent.Expect("meta-data", "exists", job.CommitMetadataKey).AndExitWith(0)
	agent.Expect("reuse", "restore").Once().AndExitWith(1)
	tester.MustMock(t, "my-command").Expect().Once().AndExitWith(3)
	agent.Expect("reuse", "save", "--exit-status=3").Once().AndExitWith(1)

	err = tester.Run(t, "BUILDKITE_STEP_REUSE=true", "BUILDKITE_COMMAND=my-command")
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("tester.Run() error = %v, want exit status 3", err)
	}
	tester.CheckMocks(t)
}
