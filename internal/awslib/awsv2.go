package awslib

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
)

// GetConfigV2 creates a new AWS SDK v2 config that uses the current region from
// IMDS, if not otherwise provided.
func GetConfigV2(ctx context.Context, optFns ...func(*config.LoadOptions) error) (cfg aws.Config, err error) {
	cfg, err = config.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		return cfg, fmt.Errorf("error loading default config: %w", err)
	}

	// local configuration resolved a region so we can return
	if cfg.Region != "" {
		return cfg, nil
	}

	// we need to fall back to the ec2 imds service to get the region
	client := imds.NewFromConfig(cfg)

	var regionResult *imds.GetRegionOutput
	regionResult, err = client.GetRegion(ctx, &imds.GetRegionInput{})
	if err != nil {
		return cfg, fmt.Errorf("error getting region using imds: %w", err)
	}

	optFns = append(optFns, config.WithRegion(regionResult.Region))

	cfg, err = config.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		return cfg, fmt.Errorf("error loading default config using imds region: %w", err)
	}

	return cfg, nil
}
