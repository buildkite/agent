package secrets

import (
	"context"
	"errors"
	"fmt"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/go-pipeline"
)

// APIClient interface defines only the method needed by the secrets manager
// to fetch secrets from the Buildkite API.
type APIClient interface {
	GetSecret(ctx context.Context, req *api.GetSecretRequest) (*api.Secret, *api.Response, error)
}

// Manager orchestrates secret fetching and processing using injected dependencies.
type Manager struct {
	client APIClient
}

// NewManager creates a new Manager with the provided API client.
func NewManager(client APIClient) *Manager {
	return &Manager{
		client: client,
	}
}

// fetchSecrets retrieves all secret values from the API sequentially.
// Uses ALL-OR-NOTHING semantics: if any secret fails, returns error with details of all failed secrets.
// Returns map of secret key to secret value only if ALL secrets fetch successfully.
func (m *Manager) fetchSecrets(ctx context.Context, jobID string, secrets []pipeline.Secret) (map[string]string, error) {
	if len(secrets) == 0 {
		return nil, nil
	}

	secretValues := make(map[string]string, len(secrets))
	errs := make([]error, 0, len(secrets))

	// Sequential fetching for initial implementation
	for _, secret := range secrets {
		apiSecret, _, err := m.client.GetSecret(ctx, &api.GetSecretRequest{
			Key:   secret.SecretKey,
			JobID: jobID,
		})
		if err != nil {
			// Include secret key name (never values) in error messages for debugging
			errs = append(errs, fmt.Errorf("secret %q: %w", secret.SecretKey, err))
			continue
		}

		secretValues[secret.SecretKey] = apiSecret.Value
	}

	// ALL-OR-NOTHING: if any secret fails, return error with details of all failed secrets
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return secretValues, nil
}

// processSecrets processes each secret sequentially to avoid race conditions with environment mutations.
// Clears secret values from memory immediately after processing each secret using secure zeroing.
func (m *Manager) processSecrets(ctx context.Context, secrets []pipeline.Secret, secretValues map[string]string, processors []Processor) error {
	if len(secrets) == 0 || len(secretValues) == 0 {
		return nil
	}

	errs := make([]error, 0, len(secrets))

	// Process each secret sequentially to avoid race conditions
	for _, secret := range secrets {
		secretValue, exists := secretValues[secret.SecretKey]
		if !exists {
			continue // Skip if not in secretValues map (shouldn't happen)
		}

		// Find a processor that supports this secret type
		var supportingProcessor Processor
		for _, processor := range processors {
			if processor.SupportsSecret(&secret) {
				supportingProcessor = processor
				break
			}
		}

		if supportingProcessor == nil {
			errs = append(errs, fmt.Errorf("secret %q: no processor supports this secret type", secret.SecretKey))
			continue
		}

		// Process the secret
		if err := supportingProcessor.ProcessSecret(ctx, &secret, secretValue); err != nil {
			errs = append(errs, fmt.Errorf("secret %q: %w", secret.SecretKey, err))
		}

		// Clear secret value from memory immediately after processing using secure zeroing
		clearString(&secretValue)
		secretValues[secret.SecretKey] = "" // Clear from map as well
	}

	return errors.Join(errs...)
}

// FetchAndProcess orchestrates the complete secrets workflow: fetch all secrets then process them.
// Uses ALL-OR-NOTHING semantics: if any step fails, the entire operation fails.
func (m *Manager) FetchAndProcess(ctx context.Context, jobID string, secrets []pipeline.Secret, processors []Processor) error {
	if len(secrets) == 0 {
		return nil // No secrets to process
	}

	// Step 1: Fetch all secrets from API
	secretValues, fetchErr := m.fetchSecrets(ctx, jobID, secrets)
	if fetchErr != nil {
		return fetchErr // ALL-OR-NOTHING: if fetchSecrets() returns error, immediately fail
	}

	// Step 2: Process all secrets
	processErr := m.processSecrets(ctx, secrets, secretValues, processors)

	// Clear all remaining secret values from memory
	for key, value := range secretValues {
		clearString(&value)
		secretValues[key] = ""
	}

	return processErr
}

// clearString securely clears the contents of a string by overwriting its backing array
func clearString(s *string) {
	if s == nil || *s == "" {
		return
	}

	// Get the backing byte array and zero it out
	b := []byte(*s)
	for i := range b {
		b[i] = 0
	}
	*s = ""
}
