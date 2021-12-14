package integration

import (
	"runtime"
	"testing"

	"github.com/buildkite/bintest/v3"
)

func TestMultilineCommandRunUnderBatch(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "windows" {
		t.Skip("batch test only applies to Windows")
	}

	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	setup := tester.MustMock(t, "Setup.cmd")
	build := tester.MustMock(t, "BuildProject.cmd")

	setup.Expect().Once()
	build.Expect().Once().AndCallFunc(func(c *bintest.Call) {
		llamas := c.GetEnv(`LLAMAS`)
		if llamas != "COOL" {
			t.Errorf("Expected LLAMAS=COOL, got %s", llamas)
			c.Exit(1)
		} else {
			c.Exit(0)
		}
	})

	tester.RunAndCheck(t, "BUILDKITE_COMMAND=Setup.cmd\nset LLAMAS=COOL\nBuildProject.cmd")

	t.Fatal("testing")
}

func TestPreExitHooksRunsAfterCommandFails(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	preExitFunc := func(c *bintest.Call) {
		cmdExitStatus := c.GetEnv(`BUILDKITE_COMMAND_EXIT_STATUS`)
		if cmdExitStatus != "1" {
			t.Errorf("Expected an exit status of 1, got %v", cmdExitStatus)
		}
		c.Exit(0)
	}

	tester.ExpectGlobalHook("pre-exit").Once().AndCallFunc(preExitFunc)
	tester.ExpectLocalHook("pre-exit").Once().AndCallFunc(preExitFunc)

	if err = tester.Run(t, "BUILDKITE_COMMAND=false"); err == nil {
		t.Fatal("Expected the bootstrap to fail")
	}

	tester.CheckMocks(t)
}
