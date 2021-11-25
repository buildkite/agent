package agent

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/defaults"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/buildkite/agent/v3/logger"
)

var regionHintEnvVar = "BUILDKITE_S3_DEFAULT_REGION"

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

	creds.SessionToken = os.Getenv("BUILDKITE_S3_SESSION_TOKEN")

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

func awsS3Session(region string) (*session.Session, error) {
	// Chicken and egg... but this is kinda how they do it in the sdk
	sess, err := session.NewSession()
	if err != nil {
		return nil, err
	}

	sess.Config.Region = aws.String(region)

	sess.Config.Credentials = credentials.NewChainCredentials(
		[]credentials.Provider{
			&credentialsProvider{},
			&credentials.EnvProvider{},
			webIdentityRoleProvider(sess),
			// EC2 and ECS meta-data providers
			defaults.RemoteCredProvider(*sess.Config, sess.Handlers),
		})

	return sess, nil
}

func webIdentityRoleProvider(sess *session.Session) *stscreds.WebIdentityRoleProvider {
	return stscreds.NewWebIdentityRoleProvider(
		sts.New(sess),
		os.Getenv("AWS_ROLE_ARN"),
		os.Getenv("AWS_ROLE_SESSION_NAME"),
		os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE"),
	)
}

func newS3Client(l logger.Logger, bucket string) (*s3.S3, error) {
	var sess *session.Session

	regionHint := os.Getenv(regionHintEnvVar)
	if regionHint != "" {
		// If there is a region hint provided, we use it unconditionally
		session, err := awsS3Session(regionHint)
		if err != nil {
			return nil, fmt.Errorf("Could not load the AWS SDK config (%v)", err)
		}

		sess = session
	} else {
		// Otherwise, use the current region (or a guess) to dynamically find
		// where the bucket lives.
		region, err := awsRegion()
		if err != nil {
			region = "us-east-1"
		}

		l.Debug("Discovered current region as %q\n", region)

		// Using the guess region, construct a session and ask that region where the
		// bucket lives
		session, err := awsS3Session(region)
		if err != nil {
			return nil, fmt.Errorf("Could not load the AWS SDK config (%v)", err)
		}

		bucketRegion, bucketRegionErr := s3manager.GetBucketRegion(aws.BackgroundContext(), session, bucket, region)
		if bucketRegionErr == nil && bucketRegion != "" {
			l.Debug("Discovered %q bucket region as %q\n", bucket, bucketRegion)
			session.Config.Region = &bucketRegion
		} else {
			l.Error("Could not discover region for bucket %q. Using the %q region as a fallback, if this is not correct configure a bucket region using the %q environment variable. (%v)\n", bucket, *sess.Config.Region, regionHintEnvVar, err)
		}

		sess = session
	}

	l.Debug("Testing AWS S3 credentials for bucket %q in region %q...", bucket, *sess.Config.Region)

	s3client := s3.New(sess)

	// Test the authentication by trying to list the first 0 objects in the bucket.
	_, err := s3client.ListObjects(&s3.ListObjectsInput{
		Bucket:  aws.String(bucket),
		MaxKeys: aws.Int64(0),
	})
	if err != nil {
		if err == credentials.ErrNoValidProvidersFoundInChain {
			hasProxy := os.Getenv("HTTP_PROXY") != "" || os.Getenv("HTTPS_PROXY") != ""
			hasNoProxyIdmsException := strings.Contains(os.Getenv("NO_PROXY"), "169.254.169.254")

			errorTitle := "Could not authenticate with AWS S3 using any of the included credential providers."

			if hasProxy && !hasNoProxyIdmsException {
				return nil, fmt.Errorf("%s Your HTTP proxy settings do not grant a NO_PROXY=169.254.169.254 exemption for the instance metadata service, instance profile credentials may not be retrievable via your HTTP proxy.", errorTitle)
			}

			return nil, fmt.Errorf("%s You can authenticate by setting Buildkite environment variables (BUILDKITE_S3_ACCESS_KEY_ID, BUILDKITE_S3_SECRET_ACCESS_KEY), AWS environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY), Web Identity environment variables (AWS_ROLE_ARN, AWS_ROLE_SESSION_NAME, AWS_WEB_IDENTITY_TOKEN_FILE), or if running on AWS EC2 ensuring network access to the EC2 Instance Metadata Service to use an instance profile’s IAM Role credentials.", errorTitle)
		}
		return nil, fmt.Errorf("Could not s3:ListObjects in your AWS S3 bucket %q in region %q: (%s)", bucket, *sess.Config.Region, err.Error())
	}

	return s3client, nil
}
