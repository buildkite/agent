package secrets

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
	"github.com/buildkite/roko"
	"golang.org/x/sync/semaphore"
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

// FetchSecretsOpt is a functional option for FetchSecrets.
type FetchSecretsOpt func(*fetchSecretsConfig)

type fetchSecretsConfig struct {
	retrySleepFunc func(time.Duration)
}

// WithRetrySleepFunc overrides the sleep function used between retries.
// This is primarily useful for unit tests.
func WithRetrySleepFunc(f func(time.Duration)) FetchSecretsOpt {
	return func(c *fetchSecretsConfig) {
		c.retrySleepFunc = f
	}
}

// FetchSecrets retrieves all secret values from the API concurrently.
// Each individual secret fetch is retried up to 3 times with exponential
// backoff on retryable errors (TLS handshake failures, timeouts, 5xx, 429).
// If any secret fails after retries, returns error with details of all failed secrets.
func FetchSecrets(ctx context.Context, l logger.Logger, client APIClient, jobID string, keys []string, concurrency int, opts ...FetchSecretsOpt) ([]Secret, []error) {
	var cfg fetchSecretsConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	secrets := make([]Secret, 0, len(keys))
	secretsMu := sync.Mutex{}

	errs := make([]error, 0, len(keys))
	errsMu := sync.Mutex{}

	sem := semaphore.NewWeighted(int64(concurrency))

	for _, key := range keys {
		if err := sem.Acquire(ctx, 1); err != nil {
			errsMu.Lock()
			errs = append(errs, fmt.Errorf("failed to acquire semaphore for key %q: %w", key, err))
			errsMu.Unlock()
			break
		}

		go func() {
			defer sem.Release(1)

			r := roko.NewRetrier(
				roko.WithMaxAttempts(3),
				roko.WithStrategy(roko.Exponential(2*time.Second, 0)),
				roko.WithJitterRange(-1*time.Second, 5*time.Second),
				roko.WithSleepFunc(cfg.retrySleepFunc),
			)

			apiSecret, err := roko.DoFunc(ctx, r, func(r *roko.Retrier) (*api.Secret, error) {
				secret, resp, err := client.GetSecret(ctx, &api.GetSecretRequest{Key: key, JobID: jobID})
				if err != nil {
					if resp != nil && api.IsRetryableStatus(resp) {
						l.Warnf("Retrying secret %q fetch after retryable HTTP status %d (%s)", key, resp.StatusCode, r)
						return nil, err
					}

					if api.IsRetryableError(err) {
						l.Warnf("Retrying secret %q fetch after retryable error: %v (%s)", key, err, r)
						return nil, err
					}

					// Non-retryable error, stop retrying
					r.Break()
					return nil, err
				}
				return secret, nil
			})
			if err != nil {
				errsMu.Lock()
				errs = append(errs, &SecretError{
					Key: key,
					Err: err,
				})
				errsMu.Unlock()
				return
			}

			secretsMu.Lock()
			defer secretsMu.Unlock()
			secrets = append(secrets, Secret{
				Key:   key,
				Value: apiSecret.Value,
			})
		}()
	}

	err := sem.Acquire(ctx, int64(concurrency)) // Wait for all goroutines to finish
	if err != nil {
		return nil, []error{fmt.Errorf("failed to acquire semaphore waiting for jobs to finish: %w", err)}
	}

	// If any secret fails, return error with details of all failed secrets
	if len(errs) > 0 {
		return nil, errs
	}

	return secrets, nil
}

// FetchErrorFix is what a captured error for failed secret fetches suggests.
const FetchErrorFix = "Check that each secret exists in the job's cluster and that this pipeline is allowed to use it."

// DescribeFetchErrors summarizes failed fetches for a captured job error. It
// returns the keys that failed and a message, starting with heading, that
// names each key with Buildkite's response, leaving out request URLs. Fetch
// errors never contain secret values.
func DescribeFetchErrors(heading string, errs []error) (failedKeys []string, message string) {
	var b strings.Builder
	b.WriteString(heading)
	for _, err := range errs {
		key, reason := "(all secrets)", err
		if secretErr := new(SecretError); errors.As(err, &secretErr) {
			failedKeys = append(failedKeys, secretErr.Key)
			key, reason = secretErr.Key, secretErr.Err
		}
		errResp := new(api.ErrorResponse)
		switch {
		case errors.As(reason, &errResp) && errResp.Response != nil && errResp.Message != "":
			fmt.Fprintf(&b, "\n- %s: %s: %s", key, errResp.Response.Status, errResp.Message)
		case errors.As(reason, &errResp) && errResp.Response != nil:
			fmt.Fprintf(&b, "\n- %s: %s", key, errResp.Response.Status)
		default:
			fmt.Fprintf(&b, "\n- %s: %v", key, reason)
		}
	}
	b.WriteString("\n" + FetchErrorFix)
	return failedKeys, b.String()
}
