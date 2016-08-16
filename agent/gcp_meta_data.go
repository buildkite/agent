package agent

import (
	"strings"

	"google.golang.org/cloud/compute/metadata"
)

type GCPMetaData struct {
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

	return result, nil
}

func machineType() (string, error) {
	machType, err := metadata.Get("instance/machine-type")
	// machType is of the form "projects/<projNum>/machineTypes/<machType>".
	if err != nil {
		return "", err
	}
	return machType[strings.LastIndex(machType, "/")+1:], nil
}
