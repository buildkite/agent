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
	tester.ExpectGlobalHook("command").Once()
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

	tester.ExpectGlobalHook("command").Once().AndExitWith(0)
	// tester.ExpectLocalHook("command").NotCalled()

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

	if err = tester.Run("BUILDKITE_COMMAND=false"); err == nil {
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
	}{
		// Note that these are all false currently. They shouldn't be, but this
		// reflects the current behaviour
		{"environment", false, false},
		{"pre-checkout", false, false},
		{"post-checkout", false, false},
		{"pre-command", false, false},
		{"checkout", false, false},
		{"command", false, false},
		{"post-command", false, false},
	}

	for _, tc := range testCases {
		t.Run(tc.failingHook, func(t *testing.T) {
			tester, err := NewBootstrapTester()
			if err != nil {
				t.Fatal(err)
			}
			defer tester.Close()

			tester.ExpectGlobalHook(tc.failingHook).
				Once().
				AndWriteToStderr("Blargh").
				AndExitWith(1)

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

			if err = tester.Run(); err == nil {
				t.Fatal("Expected the bootstrap to fail")
			}

			tester.CheckMocks(t)
		})
	}
}
