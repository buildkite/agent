package artifact

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
	"github.com/buildkite/agent/v3/internal/awslib"
	"github.com/buildkite/agent/v3/logger"
)

const (
	regionHintEnvVar = "BUILDKITE_S3_DEFAULT_REGION"
	s3EndpointEnvVar = "BUILDKITE_S3_ENDPOINT"
)

type buildkiteEnvProvider struct {
	retrieved bool
}

func (e *buildkiteEnvProvider) Retrieve() (credentials.Value, error) {
	creds := credentials.Value{}

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
		return credentials.Value{}, errors.New("BUILDKITE_S3_ACCESS_KEY_ID or BUILDKITE_S3_ACCESS_KEY not found in environment")
	}

	if creds.SecretAccessKey == "" {
		return credentials.Value{}, errors.New("BUILDKITE_S3_SECRET_ACCESS_KEY or BUILDKITE_S3_SECRET_KEY not found in environment")
	}

	e.retrieved = true
	return creds, nil
}

func (e *buildkiteEnvProvider) IsExpired() bool {
	return !e.retrieved
}

func awsS3Session(region string, l logger.Logger) (*session.Session, error) {
	// Chicken and egg... but this is kinda how they do it in the sdk
	sess, err := session.NewSession()
	if err != nil {
		return nil, err
	}

	sess.Config.Region = aws.String(region)

	sess.Config.Credentials = credentials.NewChainCredentials(
		[]credentials.Provider{
			&buildkiteEnvProvider{},
			&credentials.EnvProvider{},
			sharedCredentialsProvider(),
			webIdentityRoleProvider(sess),
			// EC2 and ECS meta-data providers
			defaults.RemoteCredProvider(*sess.Config, sess.Handlers),
		},
	)

	// An optional endpoint URL (hostname only or fully qualified URI)
	// that overrides the default generated endpoint for a client.
	// This is useful for S3-compatible servers like MinIO.
	if endpoint := os.Getenv(s3EndpointEnvVar); endpoint != "" {
		l.Debug("S3 session Endpoint from %s: %q", s3EndpointEnvVar, endpoint)
		sess.Config.Endpoint = aws.String(endpoint)

		// Configure the S3 client to use path-style addressing instead of the
		// default DNS-style “virtual hosted bucket addressing”. See:
		// - https://docs.aws.amazon.com/sdk-for-go/api/aws/#Config.WithS3ForcePathStyle
		// - https://github.com/aws/aws-sdk-go/blob/v1.44.181/aws/config.go#L118-L127
		// This is useful for S3-compatible servers like MinIO when they're deployed
		// without subdomain support.

		// AWS CLI does this by default when a custom endpoint is specified [1] so
		// we will too.
		// [1]: https://github.com/aws/aws-cli/blob/2.9.18/awscli/botocore/args.py#L414-L417
		l.Debug("S3 session S3ForcePathStyle=true because custom Endpoint specified")
		sess.Config.S3ForcePathStyle = aws.Bool(true)
	}

	return sess, nil
}

func sharedCredentialsProvider() credentials.Provider {
	// If empty SDK will default to environment variable "AWS_PROFILE"
	// or "default" if environment variable is also not set.
	awsProfile := os.Getenv("BUILDKITE_S3_PROFILE")

	return &credentials.SharedCredentialsProvider{Profile: awsProfile}
}

func webIdentityRoleProvider(sess *session.Session) *stscreds.WebIdentityRoleProvider {
	return stscreds.NewWebIdentityRoleProvider(
		sts.New(sess),
		os.Getenv("AWS_ROLE_ARN"),
		os.Getenv("AWS_ROLE_SESSION_NAME"),
		os.Getenv("AWS_WEB_IDENTITY_TOKEN_FILE"),
	)
}

func NewS3Client(l logger.Logger, bucket string) (*s3.S3, error) {
	var sess *session.Session

	regionHint := os.Getenv(regionHintEnvVar)
	if regionHint != "" {
		l.Debug("Using bucket region %q from environment variable %q", regionHint, regionHintEnvVar)
		// If there is a region hint provided, we use it unconditionally
		session, err := awsS3Session(regionHint, l)
		if err != nil {
			return nil, fmt.Errorf("Could not load the AWS SDK config (%w)", err)
		}

		sess = session
	} else {
		// Otherwise, use the current region (or a guess) to dynamically find
		// where the bucket lives.
		region, err := awslib.Region()
		if err != nil {
			region = "us-east-1"
		}

		l.Debug("Discovered current region as %q", region)

		// Using the guess region, construct a session and ask that region where the
		// bucket lives
		session, err := awsS3Session(region, l)
		if err != nil {
			return nil, fmt.Errorf("Could not load the AWS SDK config (%w)", err)
		}

		bucketRegion, bucketRegionErr := s3manager.GetBucketRegion(aws.BackgroundContext(), session, bucket, region)
		if bucketRegionErr == nil && bucketRegion != "" {
			l.Debug("Discovered %q bucket region as %q", bucket, bucketRegion)
			session.Config.Region = &bucketRegion
		} else {
			l.Error("Could not discover region for bucket %q. Using the %q region as a fallback, if this is not correct configure a bucket region using the %q environment variable. (%v)", bucket, *session.Config.Region, regionHintEnvVar, err)
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
		if errors.Is(err, credentials.ErrNoValidProvidersFoundInChain) {
			hasProxy := os.Getenv("HTTP_PROXY") != "" || os.Getenv("HTTPS_PROXY") != ""
			hasNoProxyIdmsException := strings.Contains(os.Getenv("NO_PROXY"), "169.254.169.254")

			errorTitle := "Could not authenticate with AWS S3 using any of the included credential providers."

			if hasProxy && !hasNoProxyIdmsException {
				return nil, fmt.Errorf("%s Your HTTP proxy settings do not grant a NO_PROXY=169.254.169.254 exemption for the instance metadata service, instance profile credentials may not be retrievable via your HTTP proxy.", errorTitle)
			}

			return nil, fmt.Errorf("%s You can authenticate by setting Buildkite environment variables (BUILDKITE_S3_ACCESS_KEY_ID, BUILDKITE_S3_SECRET_ACCESS_KEY, BUILDKITE_S3_PROFILE), AWS environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_PROFILE), Web Identity environment variables (AWS_ROLE_ARN, AWS_ROLE_SESSION_NAME, AWS_WEB_IDENTITY_TOKEN_FILE), or if running on AWS EC2 ensuring network access to the EC2 Instance Metadata Service to use an instance profile’s IAM Role credentials.", errorTitle)
		}
		return nil, fmt.Errorf("Could not s3:ListObjects in your AWS S3 bucket %q in region %q: (%s)", bucket, *sess.Config.Region, err.Error())
	}

	return s3client, nil
}
