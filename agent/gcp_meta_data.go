package agent

import (
	"errors"
	"strings"

	"cloud.google.com/go/compute/metadata"
)

type GCPMetaData struct {
}

// Takes a map of tags and meta-data paths to get, returns a map of tags and fetched values.
func (e GCPMetaData) GetPaths(paths map[string]string) (map[string]string, error) {
	result := make(map[string]string)

	for key, path := range paths {
		value, err := metadata.Get(path)
		if err != nil {
			return nil, err
		} else {
			result[key] = value
		}
	}

	return result, nil
}

func (e GCPMetaData) Get() (map[string]string, error) {
	result := make(map[string]string)

	instanceId, err := metadata.Get("instance/id")
	if err != nil {
		return result, err
	}
	result["gcp:instance-id"] = instanceId

	machineType, err := machineType()
	if err != nil {
		return result, err
	}
	result["gcp:machine-type"] = machineType

	preemptible, err := metadata.Get("instance/scheduling/preemptible")
	if err != nil {
		return result, err
	}
	result["gcp:preemptible"] = strings.ToLower(preemptible)

	projectId, err := metadata.ProjectID()
	if err != nil {
		return result, err
	}
	result["gcp:project-id"] = projectId

	zone, err := metadata.Zone()
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

func machineType() (string, error) {
	machType, err := metadata.Get("instance/machine-type")
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
