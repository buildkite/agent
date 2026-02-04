package agent

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
)

type EC2MetaData struct{}

// Takes a map of tags and meta-data paths to get, returns a map of tags and fetched values.
func (e EC2MetaData) GetPaths(ctx context.Context, paths map[string]string) (map[string]string, error) {
	metaData := make(map[string]string)

	c, err := newAWSClient(ctx)
	if err != nil {
		return metaData, err
	}

	for key, path := range paths {
		mdOut, err := c.GetMetadata(ctx, &imds.GetMetadataInput{Path: path})
		if err != nil {
			return nil, fmt.Errorf("fetching metadata: %w", err)
		}
		value, err := io.ReadAll(mdOut.Content)
		if err != nil {
			return nil, fmt.Errorf("reading metadata response: %w", err)
		}
		metaData[key] = string(value)
	}

	return metaData, nil
}

func (e EC2MetaData) Get(ctx context.Context) (map[string]string, error) {
	metaData := make(map[string]string)

	c, err := newAWSClient(ctx)
	if err != nil {
		return metaData, err
	}

	document, err := c.GetInstanceIdentityDocument(ctx, nil)
	if err != nil {
		return metaData, err
	}

	metaData["aws:account-id"] = document.AccountID
	metaData["aws:ami-id"] = document.ImageID
	metaData["aws:architecture"] = document.Architecture
	metaData["aws:availability-zone"] = document.AvailabilityZone
	metaData["aws:instance-id"] = document.InstanceID
	metaData["aws:instance-type"] = document.InstanceType
	metaData["aws:region"] = document.Region

	mdOut, err := c.GetMetadata(ctx, &imds.GetMetadataInput{Path: "instance-life-cycle"})
	if err != nil {
		return metaData, nil
	}
	instanceLifeCycle, err := io.ReadAll(mdOut.Content)
	if err != nil {
		return metaData, nil
	}

	metaData["aws:instance-life-cycle"] = string(instanceLifeCycle)
	return metaData, nil
}

func newAWSClient(ctx context.Context) (*imds.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading default AWS config: %w", err)
	}

	client := imds.NewFromConfig(cfg)
	return client, nil
}
