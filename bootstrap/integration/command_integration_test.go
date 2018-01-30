package integration

import (
	"testing"

	"github.com/lox/bintest/proxy"
)

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

	preExitFunc := func(c *proxy.Call) {
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
