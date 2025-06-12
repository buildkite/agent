package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/buildkite/agent/v3/internal/job"
	"github.com/buildkite/bintest/v3"
)

func TestArtifactsUploadAfterCommand(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	// Write a file in the command hook
	tester.ExpectGlobalHook("command").Once().AndCallFunc(func(c *bintest.Call) {
		if err := os.WriteFile(filepath.Join(c.Dir, "test.txt"), []byte("llamas"), 0o700); err != nil {
			t.Fatalf("os.WriteFile(test.txt, llamas, 0o700) = %v", err)
		}
		c.Exit(0)
	})

	// Mock out the artifact calls
	agent := tester.MockAgent(t)
	agent.
		Expect("meta-data", "exists", job.CommitMetadataKey).
		AndExitWith(0)
	agent.
		Expect("artifact", "upload", "llamas.txt").
		AndExitWith(0)

	tester.RunAndCheck(t, "BUILDKITE_ARTIFACT_PATHS=llamas.txt")
}

func TestArtifactsUploadAfterCommandFails(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	tester.MustMock(t, "my-command").Expect().AndCallFunc(func(c *bintest.Call) {
		err := os.WriteFile(filepath.Join(c.Dir, "test.txt"), []byte("llamas"), 0o700)
		if err != nil {
			t.Fatalf("os.WriteFile(test.txt, llamas, 0o700) = %v", err)
		}
		c.Exit(1)
	})

	// Mock out the artifact calls
	agent := tester.MockAgent(t)
	agent.
		Expect("meta-data", "exists", job.CommitMetadataKey).
		AndExitWith(0)
	agent.
		Expect("artifact", "upload", "llamas.txt").
		AndExitWith(0)

	err = tester.Run(t, "BUILDKITE_ARTIFACT_PATHS=llamas.txt", "BUILDKITE_COMMAND=my-command")
	if err == nil {
		t.Fatalf("Expected command to fail")
	}

	tester.CheckMocks(t)
}

func TestArtifactsUploadAfterCommandHookFails(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()

	// Write a file in the command hook
	tester.ExpectGlobalHook("command").Once().AndCallFunc(func(c *bintest.Call) {
		err := os.WriteFile(filepath.Join(c.Dir, "test.txt"), []byte("llamas"), 0o700)
		if err != nil {
			t.Fatalf("os.WriteFile(test.txt, llamas, 0o700) = %v", err)
		}
		c.Exit(1)
	})

	// Mock out the artifact calls
	agent := tester.MockAgent(t)
	agent.
		Expect("meta-data", "exists", job.CommitMetadataKey).
		AndExitWith(0)
	agent.
		Expect("artifact", "upload", "llamas.txt").
		AndExitWith(0)

	if err := tester.Run(t, "BUILDKITE_ARTIFACT_PATHS=llamas.txt"); err == nil {
		t.Fatalf("tester.Run(BUILDKITE_ARTIFACT_PATHS=llamas.txt) = %v, want non-nil error", err)
	}

	tester.CheckMocks(t)
}
