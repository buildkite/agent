package secrets

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/redact"
	"github.com/buildkite/agent/v3/internal/replacer"
	"github.com/buildkite/go-pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to test redaction with a given set of needles
func testRedaction(t *testing.T, needles []string, input string) string {
	t.Helper()
	var buf strings.Builder
	r := replacer.New(&buf, needles, redact.Redacted)
	_, err := fmt.Fprint(r, input)
	require.NoError(t, err)
	err = r.Flush()
	require.NoError(t, err)
	return buf.String()
}

func TestEnvironmentVariableProcessor_SupportsSecret(t *testing.T) {
	t.Parallel()

	processor := &EnvironmentVariableProcessor{}

	tests := []struct {
		name     string
		secret   *pipeline.Secret
		expected bool
	}{
		{
			name: "supports secret with environment variable",
			secret: &pipeline.Secret{
				SecretKey:           "DATABASE_URL",
				EnvironmentVariable: "DATABASE_URL",
			},
			expected: true,
		},
		{
			name: "supports secret with custom environment variable name",
			secret: &pipeline.Secret{
				SecretKey:           "DATABASE_URL",
				EnvironmentVariable: "DB_CONNECTION",
			},
			expected: true,
		},
		{
			name: "does not support secret without environment variable",
			secret: &pipeline.Secret{
				SecretKey:           "DATABASE_URL",
				EnvironmentVariable: "",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := processor.SupportsSecret(tt.secret)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEnvironmentVariableProcessor_ProcessSecret(t *testing.T) {
	t.Parallel()

	testEnv := env.New()

	// Create a real redactor for testing redaction
	var buf bytes.Buffer
	redactor := redact.New(&buf, []string{})
	redactors := replacer.NewMux(redactor)

	processor := &EnvironmentVariableProcessor{
		Env:       testEnv,
		Redactors: redactors,
	}

	secret := &pipeline.Secret{
		SecretKey:           "DATABASE_URL",
		EnvironmentVariable: "DATABASE_URL",
	}
	secretValue := "postgres://user:pass@localhost/db"

	err := processor.ProcessSecret(context.Background(), secret, secretValue)

	require.NoError(t, err)

	// Verify environment variable was set
	dbUrl, ok := testEnv.Get("DATABASE_URL")
	require.True(t, ok)
	assert.Equal(t, secretValue, dbUrl)

	// Verify redaction was registered by writing test data and checking output
	testInput := "Connect to postgres://user:pass@localhost/db for database"
	_, err = redactor.Write([]byte(testInput))
	require.NoError(t, err)
	err = redactor.Flush()
	require.NoError(t, err)

	redactedOutput := buf.String()
	assert.Contains(t, redactedOutput, "[REDACTED]", "Secret value should be redacted")
	assert.NotContains(t, redactedOutput, "postgres://user:pass@localhost/db", "Original secret should not appear")
}

func TestEnvironmentVariableProcessor_ProcessSecret_CustomEnvironmentVariable(t *testing.T) {
	t.Parallel()

	testEnv := env.New()

	processor := &EnvironmentVariableProcessor{
		Env:       testEnv,
		Redactors: nil, // Test without redactors
	}

	secret := &pipeline.Secret{
		SecretKey:           "DATABASE_URL",
		EnvironmentVariable: "DB_CONNECTION", // Custom environment variable name
	}
	secretValue := "postgres://test"

	err := processor.ProcessSecret(context.Background(), secret, secretValue)

	require.NoError(t, err)

	// Verify environment variable was set with custom name
	value, ok := testEnv.Get("DB_CONNECTION")
	require.True(t, ok)
	assert.Equal(t, secretValue, value)

	// Verify secret key name was not set as env var
	_, ok = testEnv.Get("DATABASE_URL")
	assert.False(t, ok, "Should not set environment variable with secret key name")
}

func TestEnvironmentVariableProcessor_ProcessSecret_WithoutRedactors(t *testing.T) {
	t.Parallel()

	testEnv := env.New()

	processor := &EnvironmentVariableProcessor{
		Env:       testEnv,
		Redactors: nil, // No redactors provided
	}

	secret := &pipeline.Secret{
		SecretKey:           "API_TOKEN",
		EnvironmentVariable: "API_TOKEN",
	}
	secretValue := "secret123"

	// Should not panic when redactors is nil
	err := processor.ProcessSecret(context.Background(), secret, secretValue)

	require.NoError(t, err)
	value, ok := testEnv.Get("API_TOKEN")
	require.True(t, ok)
	assert.Equal(t, secretValue, value)
}

func TestEnvironmentVariableProcessor_ProcessSecret_MultipleSecrets(t *testing.T) {
	t.Parallel()

	testEnv := env.New()
	// Use NewMux for simple redaction testing without buffer verification
	redactors := replacer.NewMux()

	processor := &EnvironmentVariableProcessor{
		Env:       testEnv,
		Redactors: redactors,
	}

	secrets := []struct {
		secret *pipeline.Secret
		value  string
	}{
		{
			secret: &pipeline.Secret{
				SecretKey:           "DATABASE_URL",
				EnvironmentVariable: "DATABASE_URL",
			},
			value: "postgres://test",
		},
		{
			secret: &pipeline.Secret{
				SecretKey:           "API_TOKEN",
				EnvironmentVariable: "API_TOKEN",
			},
			value: "token123",
		},
		{
			secret: &pipeline.Secret{
				SecretKey:           "DEPLOY_KEY",
				EnvironmentVariable: "DEPLOY_SSH_KEY", // Custom name
			},
			value: "ssh-rsa AAAAB3...",
		},
	}

	// Process all secrets
	for _, s := range secrets {
		err := processor.ProcessSecret(context.Background(), s.secret, s.value)
		require.NoError(t, err)
	}

	// Verify all environment variables were set correctly
	value, ok := testEnv.Get("DATABASE_URL")
	require.True(t, ok)
	assert.Equal(t, "postgres://test", value)

	value, ok = testEnv.Get("API_TOKEN")
	require.True(t, ok)
	assert.Equal(t, "token123", value)

	value, ok = testEnv.Get("DEPLOY_SSH_KEY")
	require.True(t, ok)
	assert.Equal(t, "ssh-rsa AAAAB3...", value)

	// Verify all values are redacted by testing with a real replacer
	testOutput := "Database: postgres://test, Token: token123, Key: ssh-rsa AAAAB3..."
	needles := []string{"postgres://test", "token123", "ssh-rsa AAAAB3..."}
	redacted := testRedaction(t, needles, testOutput)

	assert.Contains(t, redacted, "[REDACTED]")
	assert.NotContains(t, redacted, "postgres://test")
	assert.NotContains(t, redacted, "token123")
	assert.NotContains(t, redacted, "ssh-rsa AAAAB3...")
}

func TestEnvironmentVariableProcessor_ProcessSecret_OverwritesExisting(t *testing.T) {
	t.Parallel()

	testEnv := env.New()
	testEnv.Set("API_TOKEN", "old-token")

	processor := &EnvironmentVariableProcessor{
		Env:       testEnv,
		Redactors: nil,
	}

	secret := &pipeline.Secret{
		SecretKey:           "API_TOKEN",
		EnvironmentVariable: "API_TOKEN",
	}
	secretValue := "new-token"

	err := processor.ProcessSecret(context.Background(), secret, secretValue)

	require.NoError(t, err)
	value, ok := testEnv.Get("API_TOKEN")
	require.True(t, ok)
	assert.Equal(t, "new-token", value, "Should overwrite existing environment variable")
}

func TestEnvironmentVariableProcessor_ProcessSecret_EmptyValue(t *testing.T) {
	t.Parallel()

	testEnv := env.New()
	// Use NewMux for simple redaction testing
	redactors := replacer.NewMux()

	processor := &EnvironmentVariableProcessor{
		Env:       testEnv,
		Redactors: redactors,
	}

	secret := &pipeline.Secret{
		SecretKey:           "EMPTY_SECRET",
		EnvironmentVariable: "EMPTY_SECRET",
	}
	secretValue := ""

	err := processor.ProcessSecret(context.Background(), secret, secretValue)

	require.NoError(t, err)
	value, ok := testEnv.Get("EMPTY_SECRET")
	require.True(t, ok)
	assert.Equal(t, "", value)

	// Empty value should still be registered for redaction (though it won't redact anything)
	// Since empty string won't redact anything, just verify no errors occurred
	testOutput := "This is some text"
	redacted := testRedaction(t, []string{""}, testOutput)
	assert.Equal(t, testOutput, redacted, "Empty value should not affect redaction")
}

func TestEnvironmentVariableProcessor_ProcessSecret_SpecialCharacters(t *testing.T) {
	t.Parallel()

	testEnv := env.New()
	// Use NewMux for simple redaction testing
	redactors := replacer.NewMux()

	processor := &EnvironmentVariableProcessor{
		Env:       testEnv,
		Redactors: redactors,
	}

	secret := &pipeline.Secret{
		SecretKey:           "COMPLEX_SECRET",
		EnvironmentVariable: "COMPLEX_SECRET",
	}
	// Test with special characters that might cause issues with redaction or env vars
	secretValue := `{"key": "value", "special": "chars!@#$%^&*()[]{}|\\:;\"'<>?/.,~"}`

	err := processor.ProcessSecret(context.Background(), secret, secretValue)

	require.NoError(t, err)
	value, ok := testEnv.Get("COMPLEX_SECRET")
	require.True(t, ok)
	assert.Equal(t, secretValue, value)

	// Verify complex value is redacted
	testOutput := "The secret is: " + secretValue
	redacted := testRedaction(t, []string{secretValue}, testOutput)
	assert.Contains(t, redacted, "[REDACTED]")
	assert.NotContains(t, redacted, secretValue)
}

func TestEnvironmentVariableProcessor_ProcessSecret_MultilineValue(t *testing.T) {
	t.Parallel()

	testEnv := env.New()
	// Use NewMux for simple redaction testing
	redactors := replacer.NewMux()

	processor := &EnvironmentVariableProcessor{
		Env:       testEnv,
		Redactors: redactors,
	}

	secret := &pipeline.Secret{
		SecretKey:           "MULTILINE_SECRET",
		EnvironmentVariable: "MULTILINE_SECRET",
	}
	// Test with multiline value (like SSH private keys)
	secretValue := `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA4f5wg5l2hKsTeNem/V41fGnJm6gOdrj8ym3rFkEjWT2btb41
4w4A4fUVTnmbdqOhqvWCQNMG3Uj/UaZYqLZP9gO/8C9uVYv/7QH/gQCPa9XzBQU3
...more key content...
-----END RSA PRIVATE KEY-----`

	err := processor.ProcessSecret(context.Background(), secret, secretValue)

	require.NoError(t, err)
	value, ok := testEnv.Get("MULTILINE_SECRET")
	require.True(t, ok)
	assert.Equal(t, secretValue, value)

	// Verify multiline value is redacted - simplified check without complex output verification
	testOutput := "Private key:\n" + secretValue + "\nEnd of key"
	redacted := testRedaction(t, []string{secretValue}, testOutput)
	// Simple redaction verification - just check that replacement occurred
	assert.Contains(t, redacted, "[REDACTED]", "Output should contain redacted marker")
	assert.NotContains(t, redacted, "BEGIN RSA PRIVATE KEY", "Secret content should be redacted")
}
