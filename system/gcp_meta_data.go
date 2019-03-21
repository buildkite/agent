package system

import (
	"errors"
	"strings"

	"cloud.google.com/go/compute/metadata"
)

func GCPMetaData() (map[string]string, error) {
	result := make(map[string]string)

	instanceId, err := metadata.Get("instance/id")
	if err != nil {
		return result, err
	}
	result["gcp:instance-id"] = instanceId

	machineType, err := gcpMachineType()
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

	region, err := parseGCPRegionFromZone(zone)
	if err != nil {
		return result, err
	}
	result["gcp:region"] = region

	return result, nil
}

func gcpMachineType() (string, error) {
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

func parseGCPRegionFromZone(zone string) (string, error) {
	// zone is of the form "<region>-<letter>".
	index := strings.LastIndex(zone, "-")
	if index == -1 {
		return "", errors.New("cannot parse zone: " + zone)
	}
	return zone[:index], nil
}
