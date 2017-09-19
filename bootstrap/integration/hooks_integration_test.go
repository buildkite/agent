package integration

import (
	"fmt"
	"testing"

	"github.com/lox/bintest/proxy"
)

func TestCheckingOutFiresCorrectHooks(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	tester.ExpectGlobalHook("environment").Once()
	tester.ExpectLocalHook("environment").NotCalled()
	tester.ExpectGlobalHook("pre-checkout").Once()
	tester.ExpectLocalHook("pre-checkout").NotCalled()
	tester.ExpectGlobalHook("post-checkout").Once()
	tester.ExpectLocalHook("post-checkout").Once()
	tester.ExpectGlobalHook("pre-command").Once()
	tester.ExpectLocalHook("pre-command").Once()
	tester.ExpectGlobalHook("command").Once().AndExitWith(0).AndWriteToStdout("Success!\n")
	tester.ExpectGlobalHook("post-command").Once()
	tester.ExpectLocalHook("post-command").Once()
	tester.ExpectGlobalHook("pre-artifact").NotCalled()
	tester.ExpectLocalHook("pre-artifact").NotCalled()
	tester.ExpectGlobalHook("post-artifact").NotCalled()
	tester.ExpectLocalHook("post-artifact").NotCalled()
	tester.ExpectGlobalHook("pre-exit").Once()
	tester.ExpectLocalHook("pre-exit").Once()

	tester.RunAndCheck(t)
}

func TestReplacingCheckoutHook(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	// run a checkout in our checkout hook, otherwise we won't have local hooks to run
	tester.ExpectGlobalHook("checkout").Once().AndCallFunc(func(c *proxy.Call) {
		out, err := tester.Repo.Execute("clone", "-v", "--", tester.Repo.Path, c.GetEnv(`BUILDKITE_BUILD_CHECKOUT_PATH`))
		if err != nil {
			fmt.Println(out)
			c.Exit(1)
			return
		}
		fmt.Println(out)
		c.Exit(0)
	})

	tester.ExpectGlobalHook("pre-checkout").Once()
	tester.ExpectGlobalHook("post-checkout").Once()
	tester.ExpectLocalHook("post-checkout").Once()
	tester.ExpectGlobalHook("pre-exit").Once()
	tester.ExpectLocalHook("pre-exit").Once()

	tester.RunAndCheck(t)
}

func TestReplacingGlobalCommandHook(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	tester.ExpectGlobalHook("command").Once().AndExitWith(0)

	tester.ExpectGlobalHook("environment").Once()
	tester.ExpectGlobalHook("pre-checkout").Once()
	tester.ExpectGlobalHook("post-checkout").Once()
	tester.ExpectLocalHook("post-checkout").Once()
	tester.ExpectGlobalHook("pre-command").Once()
	tester.ExpectLocalHook("pre-command").Once()
	tester.ExpectGlobalHook("post-command").Once()
	tester.ExpectLocalHook("post-command").Once()
	tester.ExpectGlobalHook("pre-exit").Once()
	tester.ExpectLocalHook("pre-exit").Once()

	tester.RunAndCheck(t)
}

func TestReplacingLocalCommandHook(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	tester.ExpectLocalHook("command").Once().AndExitWith(0)
	tester.ExpectGlobalHook("command").NotCalled()

	tester.ExpectGlobalHook("environment").Once()
	tester.ExpectGlobalHook("pre-checkout").Once()
	tester.ExpectGlobalHook("post-checkout").Once()
	tester.ExpectLocalHook("post-checkout").Once()
	tester.ExpectGlobalHook("pre-command").Once()
	tester.ExpectLocalHook("pre-command").Once()
	tester.ExpectGlobalHook("post-command").Once()
	tester.ExpectLocalHook("post-command").Once()
	tester.ExpectGlobalHook("pre-exit").Once()
	tester.ExpectLocalHook("pre-exit").Once()

	tester.RunAndCheck(t)
}

func TestPreExitHooksFireAfterCommandFailures(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	tester.ExpectGlobalHook("pre-exit").Once()
	tester.ExpectLocalHook("pre-exit").Once()

	if err = tester.Run(t, "BUILDKITE_COMMAND=false"); err == nil {
		t.Fatal("Expected the bootstrap to fail")
	} else {
		t.Logf("Failed as expected with %v", err)
	}

	tester.CheckMocks(t)
}

func TestPreExitHooksFireAfterHookFailures(t *testing.T) {
	var testCases = []struct {
		failingHook         string
		expectGlobalPreExit bool
		expectLocalPreExit  bool
		expectCheckout      bool
		expectArtifacts     bool
	}{
		{"environment", true, false, false, false},
		{"pre-checkout", true, false, false, false},
		{"post-checkout", true, true, true, true},
		{"checkout", true, false, false, false},
		{"pre-command", true, true, true, true},
		{"command", true, true, true, true},
		{"post-command", true, true, true, true},
		{"pre-artifact", true, true, true, false},
		{"post-artifact", true, true, true, true},
	}

	for _, tc := range testCases {
		t.Run(tc.failingHook, func(t *testing.T) {
			tester, err := NewBootstrapTester()
			if err != nil {
				t.Fatal(err)
			}
			defer tester.Close()

			agent := tester.MustMock(t, "buildkite-agent")

			tester.ExpectGlobalHook(tc.failingHook).
				Once().
				AndWriteToStderr("Blargh\n").
				AndExitWith(1)

			if tc.expectCheckout {
				agent.
					Expect("meta-data", "exists", "buildkite:git:commit").
					AndExitWith(0)
			}

			if tc.expectGlobalPreExit {
				tester.ExpectGlobalHook("pre-exit").Once()
			} else {
				tester.ExpectGlobalHook("pre-exit").NotCalled()
			}

			if tc.expectLocalPreExit {
				tester.ExpectLocalHook("pre-exit").Once()
			} else {
				tester.ExpectGlobalHook("pre-exit").NotCalled()
			}

			if tc.expectArtifacts {
				agent.
					Expect("artifact", "upload", "test.txt").
					AndExitWith(0)
			}

			if err = tester.Run(t, "BUILDKITE_ARTIFACT_PATHS=test.txt"); err == nil {
				t.Fatal("Expected the bootstrap to fail")
			}

			tester.CheckMocks(t)
		})
	}
}
