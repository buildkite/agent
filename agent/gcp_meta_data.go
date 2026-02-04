package agent

import (
	"context"
	"errors"
	"strings"

	"cloud.google.com/go/compute/metadata"
)

type GCPMetaData struct{}

// Takes a map of tags and meta-data paths to get, returns a map of tags and fetched values.
func (e GCPMetaData) GetPaths(ctx context.Context, paths map[string]string) (map[string]string, error) {
	result := make(map[string]string)

	for key, path := range paths {
		value, err := metadata.GetWithContext(ctx, path)
		if err != nil {
			return nil, err
		} else {
			result[key] = value
		}
	}

	return result, nil
}

func (e GCPMetaData) Get(ctx context.Context) (map[string]string, error) {
	result := make(map[string]string)

	instanceId, err := metadata.GetWithContext(ctx, "instance/id")
	if err != nil {
		return result, err
	}
	result["gcp:instance-id"] = instanceId

	instanceName, err := metadata.GetWithContext(ctx, "instance/name")
	if err != nil {
		return result, err
	}
	result["gcp:instance-name"] = instanceName

	machineType, err := machineType(ctx)
	if err != nil {
		return result, err
	}
	result["gcp:machine-type"] = machineType

	preemptible, err := metadata.GetWithContext(ctx, "instance/scheduling/preemptible")
	if err != nil {
		return result, err
	}
	result["gcp:preemptible"] = strings.ToLower(preemptible)

	projectID, err := metadata.ProjectIDWithContext(ctx)
	if err != nil {
		return result, err
	}
	result["gcp:project-id"] = projectID

	zone, err := metadata.ZoneWithContext(ctx)
	if err != nil {
		return result, err
	}
	result["gcp:zone"] = zone

	region, err := parseRegionFromZone(zone)
	if err != nil {
		return result, err
	}
	result["gcp:region"] = region

	return result, nil
}

func machineType(ctx context.Context) (string, error) {
	machType, err := metadata.GetWithContext(ctx, "instance/machine-type")
	// machType is of the form "projects/<projNum>/machineTypes/<machType>".
	if err != nil {
		return "", err
	}
	index := strings.LastIndex(machType, "/")
	if index == -1 {
		return "", errors.New("cannot parse machine-type: " + machType)
	}
	return machType[index+1:], nil
}

func parseRegionFromZone(zone string) (string, error) {
	// zone is of the form "<region>-<letter>".
	index := strings.LastIndex(zone, "-")
	if index == -1 {
		return "", errors.New("cannot parse zone: " + zone)
	}
	return zone[:index], nil
}
