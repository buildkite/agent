package integration

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/lox/bintest/proxy"
)

func TestArtifactsUploadAfterCommand(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	// Write a file in the command hook
	tester.ExpectGlobalHook("command").Once().AndCallFunc(func(c *proxy.Call) {
		err := ioutil.WriteFile(filepath.Join(c.Dir, "test.txt"), []byte("llamas"), 0700)
		if err != nil {
			t.Fatalf("Write failed with %v", err)
		}
		c.Exit(0)
	})

	// Mock out the artifact calls
	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)
	agent.
		Expect("artifact", "upload", "llamas.txt").
		AndExitWith(0)

	tester.RunAndCheck(t, "BUILDKITE_ARTIFACT_PATHS=llamas.txt")
}

func TestArtifactsUploadAfterCommandFails(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	tester.MustMock(t, "my-command").Expect().AndCallFunc(func(c *proxy.Call) {
		err := ioutil.WriteFile(filepath.Join(c.Dir, "test.txt"), []byte("llamas"), 0700)
		if err != nil {
			t.Fatalf("Write failed with %v", err)
		}
		c.Exit(1)
	})

	// Mock out the artifact calls
	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)
	agent.
		Expect("artifact", "upload", "llamas.txt").
		AndExitWith(0)

	err = tester.Run(t, "BUILDKITE_ARTIFACT_PATHS=llamas.txt", "BUILDKITE_COMMAND=my-command")
	if err == nil {
		t.Fatalf("Expected command to fail")
	} else {
		t.Logf("Command failed as expected: %v", err)
	}

	tester.CheckMocks(t)
}

func TestArtifactsUploadAfterCommandHookFails(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	// Write a file in the command hook
	tester.ExpectGlobalHook("command").Once().AndCallFunc(func(c *proxy.Call) {
		err := ioutil.WriteFile(filepath.Join(c.Dir, "test.txt"), []byte("llamas"), 0700)
		if err != nil {
			t.Fatalf("Write failed with %v", err)
		}
		c.Exit(1)
	})

	// Mock out the artifact calls
	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)
	agent.
		Expect("artifact", "upload", "llamas.txt").
		AndExitWith(0)

	if err := tester.Run(t, "BUILDKITE_ARTIFACT_PATHS=llamas.txt"); err == nil {
		t.Fatal("Expected bootstrap to fail")
	}

	tester.CheckMocks(t)
}
