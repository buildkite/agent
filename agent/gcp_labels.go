package agent

import (
	"context"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/compute/v1"
)

type GCPLabels struct {
}

func (e GCPLabels) Get() (map[string]string, error) {

	ctx := context.Background()
	client, err := google.DefaultClient(ctx, compute.CloudPlatformScope)
	if err != nil {
		return nil, err
	}

	computeService, err := compute.New(client)
	if err != nil {
		return nil, err
	}

	// Grab the current instance's metadata as a convenience
	// to obtain the projectId, zone, and instanceId.
	metadata, err := GCPMetaData{}.Get()
	if err != nil {
		return nil, err
	}

	projectID := metadata["gcp:project-id"]
	zone := metadata["gcp:zone"]
	instanceID := metadata["gcp:instance-id"]

	instance, err := computeService.Instances.Get(
		projectID, zone, instanceID,
	).Context(ctx).Do()

	if err != nil {
		return nil, err
	}

	return instance.Labels, nil
}
