package secrets

import (
	"context"
	"errors"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/replacer"
	"github.com/buildkite/go-pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAPIClient implements the APIClient interface for testing
type mockAPIClient struct {
	secrets map[string]*api.Secret // map of secret key to secret response
	errors  map[string]error       // map of secret key to error to return
}

func (m *mockAPIClient) GetSecret(ctx context.Context, req *api.GetSecretRequest) (*api.Secret, *api.Response, error) {
	if err, exists := m.errors[req.Key]; exists {
		return nil, nil, err
	}

	if secret, exists := m.secrets[req.Key]; exists {
		return secret, &api.Response{}, nil
	}

	return nil, &api.Response{}, errors.New("secret not found")
}

// mockProcessor implements the Processor interface for testing
type mockProcessor struct {
	supportedKeys    []string
	processedSecrets map[string]string // map of secret key to processed value
	processingErrors map[string]error  // map of secret key to processing error
}

func (m *mockProcessor) SupportsSecret(secret *pipeline.Secret) bool {
	for _, key := range m.supportedKeys {
		if secret.SecretKey == key {
			return true
		}
	}
	return false
}

func (m *mockProcessor) ProcessSecret(ctx context.Context, secret *pipeline.Secret, value string) error {
	if err, exists := m.processingErrors[secret.SecretKey]; exists {
		return err
	}

	if m.processedSecrets == nil {
		m.processedSecrets = make(map[string]string)
	}
	m.processedSecrets[secret.SecretKey] = value
	return nil
}

func TestManager_fetchSecrets_Success(t *testing.T) {
	t.Parallel()

	client := &mockAPIClient{
		secrets: map[string]*api.Secret{
			"DATABASE_URL": {Key: "DATABASE_URL", Value: "postgres://test"},
			"API_TOKEN":    {Key: "API_TOKEN", Value: "secret123"},
		},
	}

	manager := NewManager(client)
	secrets := []pipeline.Secret{
		{SecretKey: "DATABASE_URL", EnvironmentVariable: "DATABASE_URL"},
		{SecretKey: "API_TOKEN", EnvironmentVariable: "API_TOKEN"},
	}

	secretValues, err := manager.fetchSecrets(context.Background(), "job123", secrets)

	require.NoError(t, err)
	assert.Equal(t, "postgres://test", secretValues["DATABASE_URL"])
	assert.Equal(t, "secret123", secretValues["API_TOKEN"])
}

func TestManager_fetchSecrets_PartialFailure(t *testing.T) {
	t.Parallel()

	client := &mockAPIClient{
		secrets: map[string]*api.Secret{
			"DATABASE_URL": {Key: "DATABASE_URL", Value: "postgres://test"},
		},
		errors: map[string]error{
			"API_TOKEN": errors.New("permission denied"),
		},
	}

	manager := NewManager(client)
	secrets := []pipeline.Secret{
		{SecretKey: "DATABASE_URL", EnvironmentVariable: "DATABASE_URL"},
		{SecretKey: "API_TOKEN", EnvironmentVariable: "API_TOKEN"},
	}

	secretValues, err := manager.fetchSecrets(context.Background(), "job123", secrets)

	require.Error(t, err)
	assert.Nil(t, secretValues) // ALL-OR-NOTHING: should be nil on any failure
	assert.Contains(t, err.Error(), `secret "API_TOKEN": permission denied`)
}

func TestManager_fetchSecrets_AllFailures(t *testing.T) {
	t.Parallel()

	client := &mockAPIClient{
		errors: map[string]error{
			"DATABASE_URL": errors.New("not found"),
			"API_TOKEN":    errors.New("permission denied"),
		},
	}

	manager := NewManager(client)
	secrets := []pipeline.Secret{
		{SecretKey: "DATABASE_URL", EnvironmentVariable: "DATABASE_URL"},
		{SecretKey: "API_TOKEN", EnvironmentVariable: "API_TOKEN"},
	}

	secretValues, err := manager.fetchSecrets(context.Background(), "job123", secrets)

	require.Error(t, err)
	assert.Nil(t, secretValues)
	// Should contain both error messages
	assert.Contains(t, err.Error(), `secret "DATABASE_URL": not found`)
	assert.Contains(t, err.Error(), `secret "API_TOKEN": permission denied`)
}

func TestManager_fetchSecrets_EmptySecrets(t *testing.T) {
	t.Parallel()

	client := &mockAPIClient{}
	manager := NewManager(client)

	secretValues, err := manager.fetchSecrets(context.Background(), "job123", nil)

	require.NoError(t, err)
	assert.Nil(t, secretValues)
}

func TestManager_processSecrets_Success(t *testing.T) {
	t.Parallel()

	processor := &mockProcessor{
		supportedKeys: []string{"DATABASE_URL", "API_TOKEN"},
	}

	manager := NewManager(nil)
	secrets := []pipeline.Secret{
		{SecretKey: "DATABASE_URL", EnvironmentVariable: "DATABASE_URL"},
		{SecretKey: "API_TOKEN", EnvironmentVariable: "API_TOKEN"},
	}
	secretValues := map[string]string{
		"DATABASE_URL": "postgres://test",
		"API_TOKEN":    "secret123",
	}

	err := manager.processSecrets(context.Background(), secrets, secretValues, []Processor{processor})

	require.NoError(t, err)
	assert.Equal(t, "postgres://test", processor.processedSecrets["DATABASE_URL"])
	assert.Equal(t, "secret123", processor.processedSecrets["API_TOKEN"])
}

func TestManager_processSecrets_NoSupportedProcessor(t *testing.T) {
	t.Parallel()

	processor := &mockProcessor{
		supportedKeys: []string{"DIFFERENT_KEY"}, // Doesn't support our secrets
	}

	manager := NewManager(nil)
	secrets := []pipeline.Secret{
		{SecretKey: "DATABASE_URL", EnvironmentVariable: "DATABASE_URL"},
	}
	secretValues := map[string]string{
		"DATABASE_URL": "postgres://test",
	}

	err := manager.processSecrets(context.Background(), secrets, secretValues, []Processor{processor})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `secret "DATABASE_URL": no processor supports this secret type`)
}

func TestManager_processSecrets_ProcessingError(t *testing.T) {
	t.Parallel()

	processor := &mockProcessor{
		supportedKeys: []string{"DATABASE_URL"},
		processingErrors: map[string]error{
			"DATABASE_URL": errors.New("processing failed"),
		},
	}

	manager := NewManager(nil)
	secrets := []pipeline.Secret{
		{SecretKey: "DATABASE_URL", EnvironmentVariable: "DATABASE_URL"},
	}
	secretValues := map[string]string{
		"DATABASE_URL": "postgres://test",
	}

	err := manager.processSecrets(context.Background(), secrets, secretValues, []Processor{processor})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `secret "DATABASE_URL": processing failed`)
}

func TestManager_FetchAndProcess_Success(t *testing.T) {
	t.Parallel()

	client := &mockAPIClient{
		secrets: map[string]*api.Secret{
			"DATABASE_URL": {Key: "DATABASE_URL", Value: "postgres://test"},
		},
	}

	processor := &mockProcessor{
		supportedKeys: []string{"DATABASE_URL"},
	}

	manager := NewManager(client)
	secrets := []pipeline.Secret{
		{SecretKey: "DATABASE_URL", EnvironmentVariable: "DATABASE_URL"},
	}

	err := manager.FetchAndProcess(context.Background(), "job123", secrets, []Processor{processor})

	require.NoError(t, err)
	assert.Equal(t, "postgres://test", processor.processedSecrets["DATABASE_URL"])
}

func TestManager_FetchAndProcess_FetchFailure(t *testing.T) {
	t.Parallel()

	client := &mockAPIClient{
		errors: map[string]error{
			"DATABASE_URL": errors.New("fetch failed"),
		},
	}

	processor := &mockProcessor{
		supportedKeys: []string{"DATABASE_URL"},
	}

	manager := NewManager(client)
	secrets := []pipeline.Secret{
		{SecretKey: "DATABASE_URL", EnvironmentVariable: "DATABASE_URL"},
	}

	err := manager.FetchAndProcess(context.Background(), "job123", secrets, []Processor{processor})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `secret "DATABASE_URL": fetch failed`)
	// Processing should not have occurred
	assert.Empty(t, processor.processedSecrets)
}

func TestManager_FetchAndProcess_ProcessFailure(t *testing.T) {
	t.Parallel()

	client := &mockAPIClient{
		secrets: map[string]*api.Secret{
			"DATABASE_URL": {Key: "DATABASE_URL", Value: "postgres://test"},
		},
	}

	processor := &mockProcessor{
		supportedKeys: []string{"DATABASE_URL"},
		processingErrors: map[string]error{
			"DATABASE_URL": errors.New("process failed"),
		},
	}

	manager := NewManager(client)
	secrets := []pipeline.Secret{
		{SecretKey: "DATABASE_URL", EnvironmentVariable: "DATABASE_URL"},
	}

	err := manager.FetchAndProcess(context.Background(), "job123", secrets, []Processor{processor})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `secret "DATABASE_URL": process failed`)
}

func TestManager_FetchAndProcess_EmptySecrets(t *testing.T) {
	t.Parallel()

	manager := NewManager(nil)

	err := manager.FetchAndProcess(context.Background(), "job123", nil, nil)

	require.NoError(t, err)
}

func TestClearString(t *testing.T) {
	t.Parallel()

	// Test clearing a non-empty string
	s := "sensitive-data"
	clearString(&s)
	assert.Equal(t, "", s)

	// Test clearing an empty string
	empty := ""
	clearString(&empty)
	assert.Equal(t, "", empty)

	// Test clearing a nil pointer (should not panic)
	clearString(nil)
}

// Integration test with real EnvironmentVariableProcessor
func TestManager_IntegrationWithEnvironmentProcessor(t *testing.T) {
	t.Parallel()

	client := &mockAPIClient{
		secrets: map[string]*api.Secret{
			"DATABASE_URL": {Key: "DATABASE_URL", Value: "postgres://test"},
			"API_TOKEN":    {Key: "API_TOKEN", Value: "secret123"},
		},
	}

	// Use real environment and redactor for integration test
	testEnv := env.New()
	redactors := replacer.NewMux() // Use NewMux() for the Mux type

	processor := &EnvironmentVariableProcessor{
		Env:       testEnv,
		Redactors: redactors,
	}

	manager := NewManager(client)
	secrets := []pipeline.Secret{
		{SecretKey: "DATABASE_URL", EnvironmentVariable: "DATABASE_URL"},
		{SecretKey: "API_TOKEN", EnvironmentVariable: "API_TOKEN"},
	}

	err := manager.FetchAndProcess(context.Background(), "job123", secrets, []Processor{processor})

	require.NoError(t, err)

	// Verify environment variables were set
	dbUrl, ok := testEnv.Get("DATABASE_URL")
	require.True(t, ok)
	assert.Equal(t, "postgres://test", dbUrl)

	apiToken, ok := testEnv.Get("API_TOKEN")
	require.True(t, ok)
	assert.Equal(t, "secret123", apiToken)

	// Note: In a real integration test, redaction verification would require
	// setting up actual replacers with writers, which is complex for unit tests.
	// The redactors.Add() call in the processor is tested separately.
}
