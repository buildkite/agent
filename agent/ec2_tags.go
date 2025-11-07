package agent

import (
	"context"
	"fmt"
	"io"

	"github.com/buildkite/agent/v3/internal/awslib"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type EC2Tags struct{}

func (e EC2Tags) Get(ctx context.Context) (map[string]string, error) {
	cfg, err := awslib.GetConfigV2(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading default AWS config: %w", err)
	}

	client := imds.NewFromConfig(cfg)

	// Grab the current instances id
	mdOut, err := client.GetMetadata(ctx, &imds.GetMetadataInput{
		Path: "instance-id",
	})
	if err != nil {
		return nil, fmt.Errorf("fetching metadata from IMDS: %w", err)
	}

	instanceID, err := io.ReadAll(mdOut.Content)
	if err != nil {
		return nil, fmt.Errorf("reading instance ID from metadata: %w", err)
	}

	svc := ec2.NewFromConfig(cfg)

	// Describe the tags of the current instance
	resp, err := svc.DescribeTags(ctx, &ec2.DescribeTagsInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("resource-id"),
				Values: []string{string(instanceID)},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	// Collect the tags
	tags := make(map[string]string, len(resp.Tags))
	for _, tag := range resp.Tags {
		tags[*tag.Key] = *tag.Value
	}
	return tags, nil
}
