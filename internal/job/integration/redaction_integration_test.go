package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/jobapi"
	"github.com/buildkite/bintest/v3"
)

func TestRedactorRedactsAgentToken(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("setting up executor tester: %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	tester.ExpectGlobalHook("command").AndCallFunc(func(c *bintest.Call) {
		fmt.Fprintf(c.Stderr, "The agent token is: %s\n", c.GetEnv("BUILDKITE_AGENT_ACCESS_TOKEN")) //nolint:errcheck // test helper; write error is non-actionable
		c.Exit(0)
	})

	err = tester.Run(t)
	if err != nil {
		t.Fatalf("running executor tester: %v", err)
	}

	if !strings.Contains(tester.Output, "The agent token is: [REDACTED]") {
		t.Fatalf("expected agent token to be redacted, but it wasn't. Full output: %s", tester.Output)
	}
}

func TestRedactorDoesNotRedactAgentToken_WhenNotInRedactedVars(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("setting up executor tester: %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	tester.ExpectGlobalHook("command").AndCallFunc(func(c *bintest.Call) {
		fmt.Fprintf(c.Stderr, "The agent token is: %s\n", c.GetEnv("BUILDKITE_AGENT_ACCESS_TOKEN")) //nolint:errcheck // test helper; write error is non-actionable
		c.Exit(0)
	})

	err = tester.Run(t, `BUILDKITE_REDACTED_VARS=""`)
	if err != nil {
		t.Fatalf("running executor tester: %v", err)
	}

	if !strings.Contains(tester.Output, "The agent token is: test-token-please-ignore") {
		t.Fatalf("expected agent token to be printed in full, but it wasn't. Full output: %s", tester.Output)
	}
}

func TestRedactorAdd_RedactsVarsAfterUse(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("setting up executor tester: %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	secret := "hunter2"
	tester.ExpectGlobalHook("command").AndCallFunc(redactionTestCommandHook(t, secret))

	err = tester.Run(t)
	if err != nil {
		t.Fatalf("running executor tester: %v", err)
	}

	// verify that the secret was echoed prior to calling RedactionCreate
	if !strings.Contains(tester.Output, fmt.Sprintf("The secret is: %s", secret)) {
		t.Fatalf("expected secret to be echoed prior to redaction, but it wasn't. Full output: %s", tester.Output)
	}

	// now check that the string got redacted after the call to RedactionCreate
	if !strings.Contains(tester.Output, "There should be a redacted here: [REDACTED]") {
		t.Fatalf("expected secret to be redacted after RedactionCreate, but it wasn't. Full output: %s", tester.Output)
	}
}

// this is a regression test - prior to https://github.com/buildkite/agent/pull/2794, when the executor was provided with
// an empty list of redacted vars, it would not initialise the redactors at all, meaning that later redactions (ie with
// the `buildkite-agent redactor add` command, or automatically when using `buildkite-agent secret get`) would not occur
func TestRegression_TestRedactorAdd_StillWorksWhenNoInitialRedactedVarsAreProvided(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("setting up executor tester: %v", err)
	}
	defer tester.Close() //nolint:errcheck // best-effort cleanup in test

	secret := "hunter2"
	tester.ExpectGlobalHook("command").AndCallFunc(redactionTestCommandHook(t, secret))

	err = tester.Run(t, `BUILDKITE_REDACTED_VARS=""`)
	if err != nil {
		t.Fatalf("running executor tester: %v", err)
	}

	// verify that the secret was echoed prior to calling RedactionCreate
	if !strings.Contains(tester.Output, fmt.Sprintf("The secret is: %s", secret)) {
		t.Fatalf("expected secret to be echoed prior to redaction, but it wasn't. Full output: %s", tester.Output)
	}

	// now check that the string is still unredacted after the call to RedactionCreate
	if !strings.Contains(tester.Output, "There should be a redacted here: [REDACTED]") {
		t.Fatalf("expected secret to be echoed without redaction after RedactionCreate, but it wasn't. Full output: %s", tester.Output)
	}
}

func redactionTestCommandHook(t *testing.T, secret string) func(c *bintest.Call) {
	t.Helper()

	return func(c *bintest.Call) {
		socketPath := c.GetEnv("BUILDKITE_AGENT_JOB_API_SOCKET")
		if socketPath == "" {
			t.Errorf("Expected BUILDKITE_AGENT_JOB_API_SOCKET to be set")
			c.Exit(1)
			return
		}

		socketToken := c.GetEnv("BUILDKITE_AGENT_JOB_API_TOKEN")
		if socketToken == "" {
			t.Errorf("Expected BUILDKITE_AGENT_JOB_API_TOKEN to be set")
			c.Exit(1)
			return
		}

		client, err := jobapi.NewClient(mainCtx, socketPath, socketToken)
		if err != nil {
			t.Errorf("creating Job API client: %v", err)
			c.Exit(1)
			return
		}

		fmt.Fprintf(c.Stderr, "The secret is: %s\n", secret) //nolint:errcheck // test helper; write error is non-actionable
		time.Sleep(time.Second) // let the log line be written before we add it to the redactor

		_, err = client.RedactionCreate(mainCtx, secret)
		if err != nil {
			t.Errorf("creating redaction: %v", err)
			c.Exit(1)
			return
		}

		fmt.Fprintf(c.Stderr, "There should be a redacted here: %s\n", secret) //nolint:errcheck // test helper; write error is non-actionable
		c.Exit(0)
	}
}
