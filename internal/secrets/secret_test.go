package secrets

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/api"
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
	secrets, err := FetchSecrets(context.Background(), mockClient, "test-job-id", keys, false)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(secrets) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(secrets))
	}

	// Verify secrets are returned in the same order as requested keys
	if secrets[0].Key != "DATABASE_URL" {
		t.Errorf("expected first secret key to be 'DATABASE_URL', got %q", secrets[0].Key)
	}
	if secrets[0].Value != "postgres://user:pass@host:5432/db" {
		t.Errorf("expected first secret value to be 'postgres://user:pass@host:5432/db', got %q", secrets[0].Value)
	}
	if secrets[1].Key != "API_TOKEN" {
		t.Errorf("expected second secret key to be 'API_TOKEN', got %q", secrets[1].Key)
	}
	if secrets[1].Value != "secret-token-123" {
		t.Errorf("expected second secret value to be 'secret-token-123', got %q", secrets[1].Value)
	}
}

func TestFetchSecrets_EmptyKeys(t *testing.T) {
	t.Parallel()

	mockClient := &mockAPIClient{}

	secrets, err := FetchSecrets(context.Background(), mockClient, "test-job-id", []string{}, false)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if secrets != nil {
		t.Errorf("expected nil secrets, got: %v", secrets)
	}
}

func TestFetchSecrets_NilKeys(t *testing.T) {
	t.Parallel()

	mockClient := &mockAPIClient{}

	secrets, err := FetchSecrets(context.Background(), mockClient, "test-job-id", nil, false)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if secrets != nil {
		t.Errorf("expected nil secrets, got: %v", secrets)
	}
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
	secrets, err := FetchSecrets(context.Background(), mockClient, "test-job-id", keys, false)

	// Should return error because some secrets failed
	if err == nil {
		t.Fatal("expected error, got none")
	}

	if secrets != nil {
		t.Errorf("expected nil secrets, got: %v", secrets)
	}

	// Error should contain details of all failed secrets
	if !strings.Contains(err.Error(), `secret "API_TOKEN": API token not found`) {
		t.Errorf("expected error to contain API_TOKEN failure, got: %v", err)
	}
	if !strings.Contains(err.Error(), `secret "MISSING": secret not found`) {
		t.Errorf("expected error to contain MISSING failure, got: %v", err)
	}
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
	secrets, err := FetchSecrets(context.Background(), mockClient, "test-job-id", keys, false)

	// Should return error because all secrets failed
	if err == nil {
		t.Fatal("expected error, got none")
	}

	if secrets != nil {
		t.Errorf("expected nil secrets, got: %v", secrets)
	}

	// Error should contain details of all failed secrets
	if !strings.Contains(err.Error(), `secret "API_TOKEN": API token not found`) {
		t.Errorf("expected error to contain API_TOKEN failure, got: %v", err)
	}
	if !strings.Contains(err.Error(), `secret "DATABASE_URL": database secret not found`) {
		t.Errorf("expected error to contain DATABASE_URL failure, got: %v", err)
	}
}

func TestFetchSecrets_APIClientError(t *testing.T) {
	t.Parallel()

	mockClient := &mockAPIClient{
		errors: map[string]error{
			"TEST_SECRET": errors.New("network error"),
		},
	}

	keys := []string{"TEST_SECRET"}
	secrets, err := FetchSecrets(context.Background(), mockClient, "test-job-id", keys, false)

	if err == nil {
		t.Fatal("expected error, got none")
	}

	if secrets != nil {
		t.Errorf("expected nil secrets, got: %v", secrets)
	}

	if !strings.Contains(err.Error(), `secret "TEST_SECRET": network error`) {
		t.Errorf("expected error to contain network error, got: %v", err)
	}
}
