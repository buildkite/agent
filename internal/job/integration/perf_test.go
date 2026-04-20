package integration

import "testing"

func BenchmarkNewExecutorTester(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		tester, err := NewExecutorTester(mainCtx)
		if err != nil {
			b.Fatalf("NewExecutorTester() error = %v", err)
		}
		if err := tester.Close(); err != nil {
			b.Fatalf("tester.Close() error = %v", err)
		}
	}
}

func BenchmarkFlushPendingLocalHooks(b *testing.B) {
	b.ReportAllocs()

	hooks := []string{
		"environment",
		"pre-checkout",
		"post-checkout",
		"pre-command",
		"post-command",
		"pre-artifact",
		"post-artifact",
		"pre-exit",
	}

	for b.Loop() {
		tester, err := NewExecutorTester(mainCtx)
		if err != nil {
			b.Fatalf("NewExecutorTester() error = %v", err)
		}

		for _, hook := range hooks {
			tester.ExpectLocalHook(hook).Once()
		}

		if err := tester.flushPendingLocalHooks(); err != nil {
			b.Fatalf("tester.flushPendingLocalHooks() error = %v", err)
		}
		if err := tester.Close(); err != nil {
			b.Fatalf("tester.Close() error = %v", err)
		}
	}
}
