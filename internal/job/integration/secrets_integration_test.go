package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/jobapi"
	"github.com/buildkite/bintest/v3"
	"github.com/buildkite/go-pipeline"
)

func TestSecretsIntegration_EnvironmentVariables(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("setting up executor tester: %v", err)
	}
	defer tester.Close()

	// Define test secrets
	secrets := []pipeline.Secret{
		{
			SecretKey:           "DATABASE_URL",
			EnvironmentVariable: "DATABASE_URL",
		},
		{
			SecretKey:           "API_TOKEN",
			EnvironmentVariable: "API_TOKEN",
		},
		{
			SecretKey:           "CUSTOM_SECRET",
			EnvironmentVariable: "MY_CUSTOM_VAR",
		},
	}

	// Set up BUILDKITE_JOB_SECRETS environment variable
	secretsJSON, err := json.Marshal(secrets)
	if err != nil {
		t.Fatalf("marshaling secrets: %v", err)
	}

	// Mock the secret API responses
	tester.MockSecret(t, "DATABASE_URL", "postgres://user:pass@host:5432/db")
	tester.MockSecret(t, "API_TOKEN", "secret-token-123")
	tester.MockSecret(t, "CUSTOM_SECRET", "my-custom-value")

	// Expect environment hook to verify secrets are available
	tester.ExpectGlobalHook("environment").AndCallFunc(func(c *bintest.Call) {
		// Verify each secret is available as an environment variable
		dbURL := c.GetEnv("DATABASE_URL")
		if dbURL == "" {
			fmt.Fprintf(c.Stderr, "❌ DATABASE_URL is not set\n")
			c.Exit(1)
			return
		}
		fmt.Fprintf(c.Stderr, "✅ DATABASE_URL is set: %s\n", dbURL)

		apiToken := c.GetEnv("API_TOKEN")
		if apiToken == "" {
			fmt.Fprintf(c.Stderr, "❌ API_TOKEN is not set\n")
			c.Exit(1)
			return
		}
		fmt.Fprintf(c.Stderr, "✅ API_TOKEN is set: %s\n", apiToken)

		customVar := c.GetEnv("MY_CUSTOM_VAR")
		if customVar == "" {
			fmt.Fprintf(c.Stderr, "❌ MY_CUSTOM_VAR is not set\n")
			c.Exit(1)
			return
		}
		fmt.Fprintf(c.Stderr, "✅ MY_CUSTOM_VAR is set: %s\n", customVar)

		c.Exit(0)
	})

	// Expect command hook to verify secrets persist
	tester.ExpectGlobalHook("command").AndCallFunc(func(c *bintest.Call) {
		fmt.Fprintf(c.Stderr, "Command hook sees DATABASE_URL: %s\n", c.GetEnv("DATABASE_URL"))
		c.Exit(0)
	})

	err = tester.Run(t, fmt.Sprintf("BUILDKITE_JOB_SECRETS=%s", string(secretsJSON)))
	if err != nil {
		t.Fatalf("running executor tester: %v", err)
	}

	// Verify success messages are present
	if !strings.Contains(tester.Output, "✅ DATABASE_URL is set") {
		t.Fatalf("expected DATABASE_URL to be set, but it wasn't. Full output: %s", tester.Output)
	}
	if !strings.Contains(tester.Output, "✅ API_TOKEN is set") {
		t.Fatalf("expected API_TOKEN to be set, but it wasn't. Full output: %s", tester.Output)
	}
	if !strings.Contains(tester.Output, "✅ MY_CUSTOM_VAR is set") {
		t.Fatalf("expected MY_CUSTOM_VAR to be set, but it wasn't. Full output: %s", tester.Output)
	}
}

func TestSecretsIntegration_Redaction(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("setting up executor tester: %v", err)
	}
	defer tester.Close()

	// Define test secret
	secrets := []pipeline.Secret{
		{
			SecretKey:           "SENSITIVE_TOKEN",
			EnvironmentVariable: "SENSITIVE_TOKEN",
		},
	}

	secretsJSON, err := json.Marshal(secrets)
	if err != nil {
		t.Fatalf("marshaling secrets: %v", err)
	}

	secretValue := "very-sensitive-secret-value-123"
	tester.MockSecret(t, "SENSITIVE_TOKEN", secretValue)

	tester.ExpectGlobalHook("command").AndCallFunc(func(c *bintest.Call) {
		// Print the secret value to stderr - should be redacted
		fmt.Fprintf(c.Stderr, "The sensitive token is: %s\n", c.GetEnv("SENSITIVE_TOKEN"))
		c.Exit(0)
	})

	err = tester.Run(t, fmt.Sprintf("BUILDKITE_JOB_SECRETS=%s", string(secretsJSON)))
	if err != nil {
		t.Fatalf("running executor tester: %v", err)
	}

	// Verify the secret value is redacted in the output
	if strings.Contains(tester.Output, secretValue) {
		t.Fatalf("expected secret value to be redacted, but found it in output: %s", tester.Output)
	}

	// Verify redaction marker is present
	if !strings.Contains(tester.Output, "The sensitive token is: [REDACTED]") {
		t.Fatalf("expected redacted secret marker, but didn't find it. Full output: %s", tester.Output)
	}
}

func TestSecretsIntegration_BackwardCompatibility(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("setting up executor tester: %v", err)
	}
	defer tester.Close()

	// Don't set BUILDKITE_JOB_SECRETS - should work normally
	tester.ExpectGlobalHook("environment").AndCallFunc(func(c *bintest.Call) {
		fmt.Fprintf(c.Stderr, "Environment hook executed successfully\n")
		c.Exit(0)
	})

	tester.ExpectGlobalHook("command").AndCallFunc(func(c *bintest.Call) {
		fmt.Fprintf(c.Stderr, "Command hook executed successfully\n")
		c.Exit(0)
	})

	err = tester.Run(t)
	if err != nil {
		t.Fatalf("running executor tester: %v", err)
	}

	// Verify job completes successfully without secrets
	if !strings.Contains(tester.Output, "Environment hook executed successfully") {
		t.Fatalf("expected environment hook to execute successfully. Full output: %s", tester.Output)
	}
	if !strings.Contains(tester.Output, "Command hook executed successfully") {
		t.Fatalf("expected command hook to execute successfully. Full output: %s", tester.Output)
	}
}

func TestSecretsIntegration_EmptySecretsConfiguration(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("setting up executor tester: %v", err)
	}
	defer tester.Close()

	// Set empty secrets array
	secretsJSON := "[]"

	tester.ExpectGlobalHook("environment").AndCallFunc(func(c *bintest.Call) {
		fmt.Fprintf(c.Stderr, "Environment hook executed with empty secrets\n")
		c.Exit(0)
	})

	tester.ExpectGlobalHook("command").AndCallFunc(func(c *bintest.Call) {
		fmt.Fprintf(c.Stderr, "Command hook executed with empty secrets\n")
		c.Exit(0)
	})

	err = tester.Run(t, fmt.Sprintf("BUILDKITE_JOB_SECRETS=%s", secretsJSON))
	if err != nil {
		t.Fatalf("running executor tester: %v", err)
	}

	// Verify job completes successfully with empty secrets
	if !strings.Contains(tester.Output, "Environment hook executed with empty secrets") {
		t.Fatalf("expected environment hook to execute successfully. Full output: %s", tester.Output)
	}
	if !strings.Contains(tester.Output, "Command hook executed with empty secrets") {
		t.Fatalf("expected command hook to execute successfully. Full output: %s", tester.Output)
	}
}

func TestSecretsIntegration_SecretFetchFailure(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("setting up executor tester: %v", err)
	}
	defer tester.Close()

	// Define secrets where one will fail to fetch
	secrets := []pipeline.Secret{
		{
			SecretKey:           "VALID_SECRET",
			EnvironmentVariable: "VALID_SECRET",
		},
		{
			SecretKey:           "INVALID_SECRET",
			EnvironmentVariable: "INVALID_SECRET",
		},
	}

	secretsJSON, err := json.Marshal(secrets)
	if err != nil {
		t.Fatalf("marshaling secrets: %v", err)
	}

	// Mock only one secret - the other will fail
	tester.MockSecret(t, "VALID_SECRET", "valid-value")
	// Don't mock INVALID_SECRET to simulate API failure

	// Job should fail before hooks execute due to secret fetch failure
	err = tester.Run(t, fmt.Sprintf("BUILDKITE_JOB_SECRETS=%s", string(secretsJSON)))
	if err == nil {
		t.Fatalf("expected job to fail due to secret fetch failure, but it succeeded. Full output: %s", tester.Output)
	}

	// Verify error message includes the failed secret key name
	if !strings.Contains(tester.Output, "INVALID_SECRET") {
		t.Fatalf("expected error to mention failed secret key, but it didn't. Full output: %s", tester.Output)
	}
}

func TestSecretsIntegration_MultilineSecretRedaction(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("setting up executor tester: %v", err)
	}
	defer tester.Close()

	// Define secret with multiline value
	secrets := []pipeline.Secret{
		{
			SecretKey:           "MULTILINE_SECRET",
			EnvironmentVariable: "MULTILINE_SECRET",
		},
	}

	secretsJSON, err := json.Marshal(secrets)
	if err != nil {
		t.Fatalf("marshaling secrets: %v", err)
	}

	multilineSecret := "line1\nline2\nline3"
	tester.MockSecret(t, "MULTILINE_SECRET", multilineSecret)

	tester.ExpectGlobalHook("command").AndCallFunc(func(c *bintest.Call) {
		// Print the multiline secret - should be redacted
		fmt.Fprintf(c.Stderr, "Multiline secret: %s\n", c.GetEnv("MULTILINE_SECRET"))
		c.Exit(0)
	})

	err = tester.Run(t, fmt.Sprintf("BUILDKITE_JOB_SECRETS=%s", string(secretsJSON)))
	if err != nil {
		t.Fatalf("running executor tester: %v", err)
	}

	// Verify multiline secret is redacted
	if strings.Contains(tester.Output, "line1") || strings.Contains(tester.Output, "line2") || strings.Contains(tester.Output, "line3") {
		t.Fatalf("expected multiline secret to be redacted, but found parts of it in output: %s", tester.Output)
	}

	// Verify redaction marker is present
	if !strings.Contains(tester.Output, "Multiline secret: [REDACTED]") {
		t.Fatalf("expected redacted multiline secret marker, but didn't find it. Full output: %s", tester.Output)
	}
}

func TestSecretsIntegration_LocalHookAccess(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("setting up executor tester: %v", err)
	}
	defer tester.Close()

	// Define test secret
	secrets := []pipeline.Secret{
		{
			SecretKey:           "LOCAL_HOOK_SECRET",
			EnvironmentVariable: "LOCAL_HOOK_SECRET",
		},
	}

	secretsJSON, err := json.Marshal(secrets)
	if err != nil {
		t.Fatalf("marshaling secrets: %v", err)
	}

	tester.MockSecret(t, "LOCAL_HOOK_SECRET", "local-hook-value")

	// Create a local environment hook that verifies secret access
	hookContent := "#!/bin/bash\necho \"Local hook sees secret: $LOCAL_HOOK_SECRET\""
	hookPath := filepath.Join(tester.HooksDir, "environment")
	if err := os.WriteFile(hookPath, []byte(hookContent), 0o700); err != nil {
		t.Fatalf("writing local environment hook: %v", err)
	}

	tester.ExpectGlobalHook("command").AndCallFunc(func(c *bintest.Call) {
		c.Exit(0)
	})

	err = tester.Run(t, fmt.Sprintf("BUILDKITE_JOB_SECRETS=%s", string(secretsJSON)))
	if err != nil {
		t.Fatalf("running executor tester: %v", err)
	}

	// Verify local hook had access to the secret (but redacted in output)
	if !strings.Contains(tester.Output, "Local hook sees secret: [REDACTED]") {
		t.Fatalf("expected local hook to have access to secret, but didn't find redacted output. Full output: %s", tester.Output)
	}
}

func TestSecretsIntegration_JobAPIRedactionIntegration(t *testing.T) {
	t.Parallel()

	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatalf("setting up executor tester: %v", err)
	}
	defer tester.Close()

	// Define test secret
	secrets := []pipeline.Secret{
		{
			SecretKey:           "JOB_API_SECRET",
			EnvironmentVariable: "JOB_API_SECRET",
		},
	}

	secretsJSON, err := json.Marshal(secrets)
	if err != nil {
		t.Fatalf("marshaling secrets: %v", err)
	}

	secretValue := "job-api-secret-value"
	tester.MockSecret(t, "JOB_API_SECRET", secretValue)

	tester.ExpectGlobalHook("command").AndCallFunc(func(c *bintest.Call) {
		socketPath := c.GetEnv("BUILDKITE_AGENT_JOB_API_SOCKET")
		if socketPath == "" {
			fmt.Fprintf(c.Stderr, "Expected BUILDKITE_AGENT_JOB_API_SOCKET to be set\n")
			c.Exit(1)
			return
		}

		socketToken := c.GetEnv("BUILDKITE_AGENT_JOB_API_TOKEN")
		if socketToken == "" {
			fmt.Fprintf(c.Stderr, "Expected BUILDKITE_AGENT_JOB_API_TOKEN to be set\n")
			c.Exit(1)
			return
		}

		client, err := jobapi.NewClient(mainCtx, socketPath, socketToken)
		if err != nil {
			fmt.Fprintf(c.Stderr, "creating Job API client: %v\n", err)
			c.Exit(1)
			return
		}

		// Print the secret before redaction is added via Job API
		fmt.Fprintf(c.Stderr, "Secret before explicit redaction: %s\n", c.GetEnv("JOB_API_SECRET"))
		time.Sleep(100 * time.Millisecond) // brief pause

		// Add additional redaction via Job API (should already be redacted from secrets fetch)
		_, err = client.RedactionCreate(mainCtx, secretValue)
		if err != nil {
			fmt.Fprintf(c.Stderr, "creating redaction: %v\n", err)
			c.Exit(1)
			return
		}

		// Print the secret after additional redaction
		fmt.Fprintf(c.Stderr, "Secret after explicit redaction: %s\n", c.GetEnv("JOB_API_SECRET"))
		c.Exit(0)
	})

	err = tester.Run(t, fmt.Sprintf("BUILDKITE_JOB_SECRETS=%s", string(secretsJSON)))
	if err != nil {
		t.Fatalf("running executor tester: %v", err)
	}

	// Verify both secret outputs are redacted (secrets are redacted immediately when fetched)
	if !strings.Contains(tester.Output, "Secret before explicit redaction: [REDACTED]") {
		t.Fatalf("expected secret to be redacted before explicit redaction call. Full output: %s", tester.Output)
	}
	if !strings.Contains(tester.Output, "Secret after explicit redaction: [REDACTED]") {
		t.Fatalf("expected secret to remain redacted after explicit redaction call. Full output: %s", tester.Output)
	}

	// Verify actual secret value never appears in output
	if strings.Contains(tester.Output, secretValue) {
		t.Fatalf("found actual secret value in output, redaction failed: %s", tester.Output)
	}
}
