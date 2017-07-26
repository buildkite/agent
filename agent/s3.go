package agent

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/buildkite/agent/logger"
)

const (
	envS3AccessKeyID             = "BUILDKITE_S3_ACCESS_KEY_ID"
	envS3SecretAccessKey         = "BUILDKITE_S3_SECRET_ACCESS_KEY"
	envS3DefaultRegion           = "BUILDKITE_S3_DEFAULT_REGION"
	envArtifactUploadDestination = "BUILDKITE_ARTIFACT_UPLOAD_DESTINATION"
)

type credentialsProvider struct {
	retrieved bool
}

func (e *credentialsProvider) Retrieve() (creds credentials.Value, err error) {
	e.retrieved = false

	if v := os.Getenv(envS3AccessKeyID); v != "" {
		logger.Debug("Found s3 access key id from " + envS3AccessKeyID)
		creds.AccessKeyID = v
	}

	if v := os.Getenv(envS3SecretAccessKey); v != "" {
		logger.Debug("Found s3 secret access key from " + envS3SecretAccessKey)
		creds.SecretAccessKey = v
	}

	if creds.AccessKeyID == "" {
		err = fmt.Errorf("%s not found in environment", envS3AccessKeyID)
	}

	if creds.SecretAccessKey == "" {
		err = fmt.Errorf("%s not found in environment", envS3SecretAccessKey)
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
	} else {
		var err error
		regionName, err = awsRegion()
		if err != nil {
			return "", err
		}
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

	sess, err := session.NewSession(&aws.Config{
		Credentials: awsS3Credentials(),
		Region:      aws.String(region),
	})
	if err != nil {
		return nil, err
	}

	logger.Debug("Authorizing S3 credentials and finding bucket `%s` in region `%s`...", bucket, region)

	s3client := s3.New(sess)

	// Test the authentication by trying to list the first 0 objects in the bucket.
	_, err = s3client.ListObjects(&s3.ListObjectsInput{
		Bucket:  aws.String(bucket),
		MaxKeys: aws.Int64(0),
	})
	if err != nil {
		if err == credentials.ErrNoValidProvidersFoundInChain {
			return nil, fmt.Errorf("Could not find a valid authentication strategy to connect to S3. Try setting %s and %s",
				envS3AccessKeyID, envS3SecretAccessKey)
		}
		return nil, fmt.Errorf("Failed to authenticate to bucket `%s` in region `%s` (%s)", bucket, region, err.Error())
	}

	return s3client, nil
}
