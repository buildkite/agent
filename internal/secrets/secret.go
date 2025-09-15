package secrets

import (
	"context"
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

type SecretError struct {
	Key string
	Err error
}

func (e *SecretError) Error() string {
	return fmt.Sprintf("secret %q: %s", e.Key, e.Err.Error())
}

func (e *SecretError) Unwrap() error {
	return e.Err
}

// FetchSecrets retrieves all secret values from the API sequentially.
// If any secret fails, returns error with details of all failed secrets.
func FetchSecrets(ctx context.Context, client APIClient, jobID string, keys []string, debug bool) ([]Secret, []error) {
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
			errs = append(errs, &SecretError{
				Key: key,
				Err: err,
			})
			continue
		}

		secrets = append(secrets, Secret{
			Key:   key,
			Value: apiSecret.Value,
		})
	}

	// If any secret fails, return error with details of all failed secrets
	if len(errs) > 0 {
		return nil, errs
	}

	return secrets, nil
}
