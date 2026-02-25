package artifact

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/buildkite/agent/v3/internal/awslib"
	"github.com/buildkite/agent/v3/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	awshttp "github.com/aws/smithy-go/transport/http"
)

const (
	regionHintEnvVar = "BUILDKITE_S3_DEFAULT_REGION"
	s3EndpointEnvVar = "BUILDKITE_S3_ENDPOINT"
)

type buildkiteEnvProvider struct {
	next aws.CredentialsProvider
}

func (p buildkiteEnvProvider) Retrieve(ctx context.Context) (aws.Credentials, error) {
	creds := aws.Credentials{
		CanExpire:       false,
		AccessKeyID:     cmp.Or(os.Getenv("BUILDKITE_S3_ACCESS_KEY_ID"), os.Getenv("BUILDKITE_S3_ACCESS_KEY")),
		SecretAccessKey: cmp.Or(os.Getenv("BUILDKITE_S3_SECRET_ACCESS_KEY"), os.Getenv("BUILDKITE_S3_SECRET_KEY")),
		SessionToken:    os.Getenv("BUILDKITE_S3_SESSION_TOKEN"),
		Source:          "buildkiteEnvProvider",
	}

	if creds.AccessKeyID == "" || creds.SecretAccessKey == "" {
		// Fall back to the default provider.
		return p.next.Retrieve(ctx)
	}

	return creds, nil
}

func awsS3Config(ctx context.Context, region string) (aws.Config, error) {
	profile := cmp.Or(os.Getenv("BUILDKITE_S3_PROFILE"), os.Getenv("AWS_PROFILE"))

	cfg, err := awslib.GetConfigV2(ctx,
		config.WithRegion(region),
		config.WithSharedConfigProfile(profile),
	)
	if err != nil {
		return aws.Config{}, err
	}

	// Wrap the default credential provider in buildkiteEnvProvider
	// (Buildkite env vars get first bite of the AWS config cherry).
	cfg.Credentials = buildkiteEnvProvider{next: cfg.Credentials}

	return cfg, nil
}

func NewS3Client(ctx context.Context, l logger.Logger, bucket string) (*s3.Client, error) {
	var cfg aws.Config

	regionHint := os.Getenv(regionHintEnvVar)
	if regionHint != "" {
		l.Debug("Using bucket region %q from environment variable %q", regionHint, regionHintEnvVar)
		// If there is a region hint provided, we use it unconditionally
		tempCfg, err := awsS3Config(ctx, regionHint)
		if err != nil {
			return nil, fmt.Errorf("could not load the AWS SDK config: %w", err)
		}
		cfg = tempCfg
	} else {
		// Two-stage method. First, create a client for the current/default
		// region, then use that to ask S3 where the bucket is.
		tempCfg, err := awsS3Config(ctx, "")
		if err != nil {
			return nil, fmt.Errorf("could not load the AWS SDK config: %w", err)
		}
		cfg = tempCfg

		l.Debug("Discovered current region as %q", cfg.Region)

		client := s3.NewFromConfig(cfg)

		bucketRegion, err := manager.GetBucketRegion(ctx, client, bucket)
		if err != nil || bucketRegion == "" {
			l.Error(
				"Could not discover region for bucket %q. Using the %q region as a fallback, if this is not correct configure a bucket region using the %q environment variable. (%v)",
				bucket, cfg.Region, regionHintEnvVar, err,
			)
		} else {
			l.Debug("Discovered %q bucket region as %q", bucket, bucketRegion)
			cfg.Region = bucketRegion
		}
	}

	// An optional endpoint URL (hostname only or fully qualified URI)
	// that overrides the default generated endpoint for a client.
	// This is useful for S3-compatible servers like MinIO.
	usePathStyle := false
	if endpoint := os.Getenv(s3EndpointEnvVar); endpoint != "" {
		l.Debug("S3 session Endpoint from %s: %q", s3EndpointEnvVar, endpoint)
		cfg.BaseEndpoint = aws.String(endpoint)

		// Configure the S3 client to use path-style addressing instead of the
		// default DNS-style “virtual hosted bucket addressing”. See:
		// - https://docs.aws.amazon.com/sdk-for-go/api/aws/#Config.WithS3ForcePathStyle
		// - https://github.com/aws/aws-sdk-go/blob/v1.44.181/aws/config.go#L118-L127
		// This is useful for S3-compatible servers like MinIO when they're deployed
		// without subdomain support.

		// AWS CLI does this by default when a custom endpoint is specified [1] so
		// we will too.
		// [1]: https://github.com/aws/aws-cli/blob/2.9.18/awscli/botocore/args.py#L414-L417
		l.Debug("S3 UsePathStyle=true because custom Endpoint specified")
		usePathStyle = true
	}

	s3client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = usePathStyle
	})

	l.Debug("Testing AWS S3 credentials for bucket %q in region %q...", bucket, cfg.Region)

	// Test the authentication by trying to list the first 0 objects in the bucket.
	_, err := s3client.ListObjects(ctx, &s3.ListObjectsInput{
		Bucket:  aws.String(bucket),
		MaxKeys: aws.Int32(0),
	})

	if isAWSAuthFailure(err) {
		hasProxy := os.Getenv("HTTP_PROXY") != "" || os.Getenv("HTTPS_PROXY") != ""
		hasNoProxyIdmsException := strings.Contains(os.Getenv("NO_PROXY"), "169.254.169.254")

		const errorTitle = "could not authenticate to AWS S3 using any of the included credential providers."

		if hasProxy && !hasNoProxyIdmsException {
			return nil, fmt.Errorf("%s your HTTP proxy settings do not grant a NO_PROXY=169.254.169.254 exemption for the instance metadata service, instance profile credentials may not be retrievable via your HTTP proxy", errorTitle)
		}

		return nil, fmt.Errorf("%s you can authenticate by setting Buildkite environment variables (BUILDKITE_S3_ACCESS_KEY_ID, BUILDKITE_S3_SECRET_ACCESS_KEY, BUILDKITE_S3_PROFILE), AWS environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_PROFILE), Web Identity environment variables (AWS_ROLE_ARN, AWS_ROLE_SESSION_NAME, AWS_WEB_IDENTITY_TOKEN_FILE), or if running on AWS EC2 ensuring network access to the EC2 Instance Metadata Service to use an instance profile’s IAM Role credentials", errorTitle)
	}
	if err != nil {
		return nil, fmt.Errorf("could not s3:ListObjects in your AWS S3 bucket %q in region %q: %w", bucket, cfg.Region, err)
	}

	return s3client, nil
}

func isAWSAuthFailure(err error) bool {
	var respErr *awshttp.ResponseError
	if errors.As(err, &respErr) {
		return respErr.HTTPStatusCode() == http.StatusForbidden
	}
	return false
}
