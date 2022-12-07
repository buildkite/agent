package agent

import (
	"context"
	"fmt"
	metadata "github.com/brunoscheufler/aws-ecs-metadata-go"
	"net/http"
)

type ECSMetadata struct {
}

func (e ECSMetadata) Get() (map[string]string, error) {
	metaData := make(map[string]string)

	ecsMeta, err := metadata.GetContainer(context.Background(), &http.Client{})
	if err != nil {
		return metaData, err
	}

	switch m := ecsMeta.(type) {
	case *metadata.ContainerMetadataV3:
		metaData["ecs:container-name"] = m.DockerName
		metaData["ecs:image"] = m.Image
		metaData["ecs:task-arn"] = m.Labels.EcsTaskArn
	case *metadata.ContainerMetadataV4:
		metaData["ecs:container-name"] = m.DockerName
		metaData["ecs:image"] = m.Image
		metaData["ecs:task-arn"] = m.Labels.EcsTaskArn
	default:
		return metaData, fmt.Errorf("ecs metadata returned unknown type %T", m)
	}

	return metaData, nil
}
