package agent

import (
	"errors"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/buildkite/agent/logger"
)

type BuildkiteAWSCredentialsProvider struct {
	retrieved bool
}

func (e *BuildkiteAWSCredentialsProvider) Retrieve() (creds credentials.Value, err error) {
	e.retrieved = false

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

	e.retrieved = true

	return
}

func (e *BuildkiteAWSCredentialsProvider) IsExpired() bool {
	return !e.retrieved
}

func awsS3Credentials() *credentials.Credentials {
	return credentials.NewChainCredentials(
		[]credentials.Provider{
			&BuildkiteAWSCredentialsProvider{},
			&credentials.EnvProvider{},
			&ec2rolecreds.EC2RoleProvider{},
		})
}

func newS3Client(bucket string) (*s3.S3, error) {
	// Generate the AWS config used by the S3 client
	region := awsS3RegionFromEnv()
	config := &aws.Config{Credentials: awsS3Credentials(), Region: aws.String(region)}

	logger.Debug("Authorizing S3 credentials and finding bucket `%s` in region `%s`...", bucket, region)

	// Create the S3 client
	s3client := s3.New(config)

	// Test the authentication by trying to list the first 0 objects in the
	// bucket.
	params := &s3.ListObjectsInput{
		Bucket:  aws.String(bucket),
		MaxKeys: aws.Int64(0),
	}
	_, err := s3client.ListObjects(params)
	if err != nil {
		if err == credentials.ErrNoValidProvidersFoundInChain {
			return nil, fmt.Errorf("Could not find a valid authentication strategy to connect to S3. Try setting BUILDKITE_S3_ACCESS_KEY and BUILDKITE_S3_SECRET_KEY")
		}

		return nil, fmt.Errorf("Failed to authenticate to bucket `%s` in region `%s` (%s)", bucket, region, err.Error())
	}

	return s3client, nil
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
