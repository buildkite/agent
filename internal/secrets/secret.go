package secrets

import (
	"context"
	"errors"
	"fmt"

	"github.com/buildkite/agent/v3/api"
)

// APIClient interface defines only the method needed by the secrets manager
// to fetch secrets from the Buildkite API.
type APIClient interface {
	GetSecret(ctx context.Context, req *api.GetSecretRequest) (*api.Secret, *api.Response, error)
}

// Secret represents a fetched secret with its key and value.
type Secret struct {
	Key   string
	Value string
}

// FetchSecrets retrieves all secret values from the API sequentially.
// If any secret fails, returns error with details of all failed secrets.
func FetchSecrets(ctx context.Context, client APIClient, jobID string, keys []string) ([]Secret, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	secrets := make([]Secret, 0, len(keys))
	errs := make([]error, 0, len(keys))

	for _, key := range keys {
		apiSecret, _, err := client.GetSecret(ctx, &api.GetSecretRequest{
			Key:   key,
			JobID: jobID,
		})
		if err != nil {
			// Include secret key name (never values) in error messages for debugging
			errs = append(errs, fmt.Errorf("secret %q: %w", key, err))
			continue
		}

		secrets = append(secrets, Secret{
			Key:   key,
			Value: apiSecret.Value,
		})
	}

	// If any secret fails, return error with details of all failed secrets
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return secrets, nil
}
