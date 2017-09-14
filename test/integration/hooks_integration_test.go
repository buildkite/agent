package integration

import "testing"

func TestCheckingOutFiresCorrectHooks(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}

	// run actual git
	if err = tester.LinkLocalCommand("git"); err != nil {
		t.Fatal(err)
	}

	tester.ExpectGlobalHook("environment").Once()
	tester.ExpectLocalHook("environment").NotCalled()
	tester.ExpectGlobalHook("pre-checkout").Once()
	tester.ExpectLocalHook("pre-checkout").NotCalled()
	tester.ExpectGlobalHook("post-checkout").Once()
	tester.ExpectLocalHook("post-checkout").Once()
	tester.ExpectGlobalHook("pre-command").Once()
	tester.ExpectLocalHook("pre-command").Once()
	tester.ExpectGlobalHook("post-command").Once()
	tester.ExpectLocalHook("post-command").Once()
	tester.ExpectGlobalHook("pre-artifact").NotCalled()
	tester.ExpectLocalHook("pre-artifact").NotCalled()
	tester.ExpectGlobalHook("post-artifact").NotCalled()
	tester.ExpectLocalHook("post-artifact").NotCalled()
	tester.ExpectGlobalHook("pre-exit").Once()
	tester.ExpectLocalHook("pre-exit").Once()

	if err = tester.Run(); err != nil {
		t.Fatal(err)
	}

	if err := tester.CheckMocksAndClose(t); err != nil {
		t.Fatal(err)
	}
}
