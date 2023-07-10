package agent

import (
	"context"
	"fmt"
	metadata "github.com/brunoscheufler/aws-ecs-metadata-go"
	"net/http"
	"strconv"
)

type ECSMetadata struct {
}

func (e ECSMetadata) Get() (map[string]string, error) {
	metaData := make(map[string]string)

	taskMeta, err := metadata.GetTask(context.Background(), &http.Client{})
	if err != nil {
		return metaData, err
	}

	switch m := taskMeta.(type) {
	case *metadata.TaskMetadataV3:
		metaData["ecs:task-arn"] = m.TaskARN
		if m.Limits.CPU != 0 {
			metaData["ecs:cpu-limit"] = strconv.FormatFloat(m.Limits.CPU, 'f', -1, 64)
		}
		if m.Limits.Memory != 0 {
			metaData["ecs:memory-limit"] = strconv.Itoa(m.Limits.Memory)
		}
	case *metadata.TaskMetadataV4:
		// This might be missing on some versions of Fargate which
		// seems to unmarshal as "true"
		if m.AvailabilityZone != "true" {
			metaData["ecs:availability-zone"] = m.AvailabilityZone
		}
		metaData["ecs:launch-type"] = m.LaunchType
		metaData["ecs:task-arn"] = m.TaskARN
		if m.Limits.CPU != 0 {
			metaData["ecs:cpu-limit"] = strconv.FormatFloat(m.Limits.CPU, 'f', -1, 64)
		}
		if m.Limits.Memory != 0 {
			metaData["ecs:memory-limit"] = strconv.Itoa(m.Limits.Memory)
		}
	default:
		return metaData, fmt.Errorf("ecs metadata returned unknown type %T", m)
	}

	containerMeta, err := metadata.GetContainer(context.Background(), &http.Client{})
	if err != nil {
		return metaData, err
	}

	switch m := containerMeta.(type) {
	case *metadata.ContainerMetadataV3:
		metaData["ecs:container-name"] = m.DockerName
		metaData["ecs:image"] = m.Image
	case *metadata.ContainerMetadataV4:
		metaData["ecs:container-name"] = m.DockerName
		metaData["ecs:image"] = m.Image
	default:
		return metaData, fmt.Errorf("ecs metadata returned unknown type %T", m)
	}

	return metaData, nil
}
