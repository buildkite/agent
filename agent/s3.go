package agent

import (
	"errors"
	"os"

	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/ec2rolecreds"
	"github.com/buildkite/agent/logger"
)

type BuildkiteAWSCredentialsProvider struct{}

func (m *BuildkiteAWSCredentialsProvider) Retrieve() (creds credentials.Value, err error) {
	creds.AccessKeyID = os.Getenv("BUILDKITE_S3_ACCESS_KEY_ID")
	if creds.AccessKeyID == "" {
		creds.AccessKeyID = os.Getenv("BUILDKITE_S3_ACCESS_KEY")
	}

	creds.SecretAccessKey = os.Getenv("BUILDKITE_S3_SECRET_ACCESS_KEY")
	if creds.SecretAccessKey == "" {
		creds.SecretAccessKey = os.Getenv("BUILDKITE_S3_SECRET_KEY")
	}

	if creds.AccessKeyID == "" {
		err = errors.New("BUILDKITE_S3_ACCESS_KEY_ID or BUILDKITE_S3_ACCESS_KEY not found in environment")
	}
	if creds.SecretAccessKey == "" {
		err = errors.New("BUILDKITE_S3_SECRET_ACCESS_KEY or BUILDKITE_S3_SECRET_KEY not found in environment")
	}

	return
}

// The custom BUILDKITE_ env vars never expire
func (m *BuildkiteAWSCredentialsProvider) IsExpired() bool {
	return false
}

func awsS3Credentials() *credentials.Credentials {
	return credentials.NewChainCredentials(
		[]credentials.Provider{
			&BuildkiteAWSCredentialsProvider{},
			&credentials.EnvProvider{},
			&ec2rolecreds.EC2RoleProvider{},
		})
}

func awsS3RegionFromEnv() string {
	if os.Getenv("BUILDKITE_S3_DEFAULT_REGION") != "" {
		return os.Getenv("BUILDKITE_S3_DEFAULT_REGION")
	} else if os.Getenv("BUILDKITE_S3_REGION") != "" {
		return os.Getenv("BUILDKITE_S3_REGION")
	} else if os.Getenv("AWS_DEFAULT_REGION") != "" {
		return os.Getenv("AWS_DEFAULT_REGION")
	}

	return "us-east-1"
}

func awsS3PermissionFromEnv() string {
	permission := "public-read"
	if os.Getenv("BUILDKITE_S3_ACL") != "" {
		permission = os.Getenv("BUILDKITE_S3_ACL")
	} else if os.Getenv("AWS_S3_ACL") != "" {
		permission = os.Getenv("AWS_S3_ACL")
	}

	// The dirtiest validation method ever...
	if permission != "private" &&
		permission != "public-read" &&
		permission != "public-read-write" &&
		permission != "authenticated-read" &&
		permission != "bucket-owner-read" &&
		permission != "bucket-owner-full-control" {
		logger.Fatal("Invalid S3 ACL `%s`", permission)
	}

	return permission
}
