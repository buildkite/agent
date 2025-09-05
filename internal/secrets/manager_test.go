package secrets

import (
	"context"
	"errors"
	"testing"

	"github.com/buildkite/agent/v3/api"
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

func TestFetchSecrets_Success(t *testing.T) {
	t.Parallel()

	mockClient := &mockAPIClient{
		secrets: map[string]*api.Secret{
			"DATABASE_URL": {Key: "DATABASE_URL", Value: "postgres://user:pass@host:5432/db"},
			"API_TOKEN":    {Key: "API_TOKEN", Value: "secret-token-123"},
		},
	}

	keys := []string{"DATABASE_URL", "API_TOKEN"}
	secrets, err := FetchSecrets(context.Background(), mockClient, "test-job-id", keys)

	require.NoError(t, err)
	require.Len(t, secrets, 2)

	// Verify secrets are returned in the same order as requested keys
	assert.Equal(t, "DATABASE_URL", secrets[0].Key)
	assert.Equal(t, "postgres://user:pass@host:5432/db", secrets[0].Value)
	assert.Equal(t, "API_TOKEN", secrets[1].Key)
	assert.Equal(t, "secret-token-123", secrets[1].Value)
}

func TestFetchSecrets_EmptyKeys(t *testing.T) {
	t.Parallel()

	mockClient := &mockAPIClient{}

	secrets, err := FetchSecrets(context.Background(), mockClient, "test-job-id", []string{})

	require.NoError(t, err)
	assert.Nil(t, secrets)
}

func TestFetchSecrets_NilKeys(t *testing.T) {
	t.Parallel()

	mockClient := &mockAPIClient{}

	secrets, err := FetchSecrets(context.Background(), mockClient, "test-job-id", nil)

	require.NoError(t, err)
	assert.Nil(t, secrets)
}

func TestFetchSecrets_AllOrNothing_SomeSecretsFail(t *testing.T) {
	t.Parallel()

	mockClient := &mockAPIClient{
		secrets: map[string]*api.Secret{
			"DATABASE_URL": {Key: "DATABASE_URL", Value: "postgres://user:pass@host:5432/db"},
		},
		errors: map[string]error{
			"API_TOKEN": errors.New("API token not found"),
			"MISSING":   errors.New("secret not found"),
		},
	}

	keys := []string{"DATABASE_URL", "API_TOKEN", "MISSING"}
	secrets, err := FetchSecrets(context.Background(), mockClient, "test-job-id", keys)

	// Should return error because some secrets failed
	require.Error(t, err)
	assert.Nil(t, secrets)

	// Error should contain details of all failed secrets
	assert.Contains(t, err.Error(), `secret "API_TOKEN": API token not found`)
	assert.Contains(t, err.Error(), `secret "MISSING": secret not found`)
}

func TestFetchSecrets_AllOrNothing_AllSecretsFail(t *testing.T) {
	t.Parallel()

	mockClient := &mockAPIClient{
		errors: map[string]error{
			"API_TOKEN":    errors.New("API token not found"),
			"DATABASE_URL": errors.New("database secret not found"),
		},
	}

	keys := []string{"API_TOKEN", "DATABASE_URL"}
	secrets, err := FetchSecrets(context.Background(), mockClient, "test-job-id", keys)

	// Should return error because all secrets failed
	require.Error(t, err)
	assert.Nil(t, secrets)

	// Error should contain details of all failed secrets
	assert.Contains(t, err.Error(), `secret "API_TOKEN": API token not found`)
	assert.Contains(t, err.Error(), `secret "DATABASE_URL": database secret not found`)
}

func TestFetchSecrets_APIClientError(t *testing.T) {
	t.Parallel()

	mockClient := &mockAPIClient{
		errors: map[string]error{
			"TEST_SECRET": errors.New("network error"),
		},
	}

	keys := []string{"TEST_SECRET"}
	secrets, err := FetchSecrets(context.Background(), mockClient, "test-job-id", keys)

	require.Error(t, err)
	assert.Nil(t, secrets)
	assert.Contains(t, err.Error(), `secret "TEST_SECRET": network error`)
}
