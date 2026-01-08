package gcplib

import (
	"context"
	"fmt"

	kms "cloud.google.com/go/kms/apiv1"
	"google.golang.org/api/option"
)

// Config holds the GCP configuration needed for KMS operations.
type Config struct {
	// ClientOptions are options to pass to the GCP client.
	ClientOptions []option.ClientOption
}

// GetConfig creates a GCP configuration that uses Application Default Credentials.
// Additional client options can be provided via optFns.
func GetConfig(ctx context.Context, optFns ...option.ClientOption) (*Config, error) {
	// GCP will automatically use Application Default Credentials (ADC)
	// which can be set via:
	// - GOOGLE_APPLICATION_CREDENTIALS environment variable
	// - gcloud auth application-default login
	// - Compute Engine/GKE service account

	return &Config{
		ClientOptions: optFns,
	}, nil
}

// NewKMSClient creates a new KMS client using the configuration.
func (c *Config) NewKMSClient(ctx context.Context) (*kms.KeyManagementClient, error) {
	client, err := kms.NewKeyManagementClient(ctx, c.ClientOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to create KMS client: %w", err)
	}
	return client, nil
}
