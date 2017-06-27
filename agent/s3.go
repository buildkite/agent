package agent

import (
	"errors"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/buildkite/agent/logger"
)

type credentialsProvider struct {
	retrieved bool
}

func (e *credentialsProvider) Retrieve() (creds credentials.Value, err error) {
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

func (e *credentialsProvider) IsExpired() bool {
	return !e.retrieved
}

func awsS3Credentials() *credentials.Credentials {
	return credentials.NewChainCredentials(
		[]credentials.Provider{
			&credentialsProvider{},
			&credentials.EnvProvider{},
			&ec2rolecreds.EC2RoleProvider{},
		})
}

func awsS3RegionFromEnv() (region string, err error) {
	regionName := "us-east-1"
	if os.Getenv("BUILDKITE_S3_DEFAULT_REGION") != "" {
		regionName = os.Getenv("BUILDKITE_S3_DEFAULT_REGION")
	} else if os.Getenv("AWS_DEFAULT_REGION") != "" {
		regionName = os.Getenv("AWS_DEFAULT_REGION")
	}

	// Check to make sure the region exists.
	resolver := endpoints.DefaultResolver()
	partitions := resolver.(endpoints.EnumPartitions).Partitions()

	for _, p := range partitions {
		for id := range p.Regions() {
			if id == regionName {
				return regionName, nil
			}
		}
	}

	return "", fmt.Errorf("Unknown AWS S3 Region %q", regionName)
}

func newS3Client(bucket string) (*s3.S3, error) {
	// Generate the AWS config used by the S3 client
	region, err := awsS3RegionFromEnv()
	if err != nil {
		return nil, err
	}

	sess := session.Must(session.NewSession(&aws.Config{
		Credentials: awsS3Credentials(),
		Region:      aws.String(region),
	}))

	logger.Debug("Authorizing S3 credentials and finding bucket `%s` in region `%s`...", bucket, region)

	s3client := s3.New(sess)

	// Test the authentication by trying to list the first 0 objects in the bucket.
	_, err = s3client.ListObjects(&s3.ListObjectsInput{
		Bucket:  aws.String(bucket),
		MaxKeys: aws.Int64(0),
	})
	if err != nil {
		if err == credentials.ErrNoValidProvidersFoundInChain {
			return nil, fmt.Errorf("Could not find a valid authentication strategy to connect to S3. Try setting BUILDKITE_S3_ACCESS_KEY and BUILDKITE_S3_SECRET_KEY")
		}
		return nil, fmt.Errorf("Failed to authenticate to bucket `%s` in region `%s` (%s)", bucket, region, err.Error())
	}

	return s3client, nil
}
