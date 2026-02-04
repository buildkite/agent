package agent

import (
	"context"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

type GCPLabels struct{}

func (e GCPLabels) Get(ctx context.Context) (map[string]string, error) {
	client, err := google.DefaultClient(ctx, compute.CloudPlatformScope)
	if err != nil {
		return nil, err
	}

	computeService, err := compute.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, err
	}

	// Grab the current instance's metadata as a convenience
	// to obtain the projectId, zone, and instanceId.
	meta, err := GCPMetaData{}.Get(ctx)
	if err != nil {
		return nil, err
	}

	instance, err := computeService.Instances.Get(
		meta["gcp:project-id"],
		meta["gcp:zone"],
		meta["gcp:instance-name"],
	).Context(ctx).Do()
	if err != nil {
		return nil, err
	}

	return instance.Labels, nil
}
