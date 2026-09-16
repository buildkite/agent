// Package gcpsecrets retrieves secret payloads from Google Secret Manager.
package gcpsecrets

import (
	"context"
	"fmt"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/api/option"
)

// AccessSecretVersion retrieves the payload of a secret version from Google Secret
// Manager. name must be the full resource name of the secret version, e.g.
// "projects/*/secrets/*/versions/*". Additional client options can be provided via
// opts; if none are provided, GCP will automatically use Application Default
// Credentials (ADC).
func AccessSecretVersion(ctx context.Context, name string, opts ...option.ClientOption) ([]byte, error) {
	client, err := secretmanager.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Secret Manager client: %w", err)
	}
	defer func() { _ = client.Close() }()

	resp, err := client.AccessSecretVersion(ctx, &secretmanagerpb.AccessSecretVersionRequest{
		Name: name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to access secret version %q: %w", name, err)
	}

	return resp.GetPayload().GetData(), nil
}
