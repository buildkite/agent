package agent

import (
	"errors"
	"os"
	"time"

	"github.com/AdRoll/goamz/aws"
)

func awsS3Auth() (aws.Auth, error) {
	// First try to authenticate using the BUILDKITE_ ENV variables
	buildkiteAuth, buildkiteErr := buildkiteS3EnvAuth()
	if buildkiteErr == nil {
		return buildkiteAuth, nil
	}

	// Passing blank values here instructs the AWS library to look at the
	// current instances meta data for the security credentials.
	awsAuth, awsErr := aws.GetAuth("", "", "", time.Time{})
	if awsErr == nil {
		return awsAuth, nil
	}

	var err error

	// If they attempted to use the BUILDKITE_ ENV variables, return them
	// that error, otherwise default to the error from AWS
	if buildkiteErr != nil && buildkiteAuth.AccessKey != "" || buildkiteAuth.SecretKey != "" {
		err = buildkiteErr
	} else {
		err = awsErr
	}

	return aws.Auth{}, err
}

func buildkiteS3EnvAuth() (auth aws.Auth, err error) {
	auth.AccessKey = os.Getenv("BUILDKITE_S3_ACCESS_KEY_ID")
	if auth.AccessKey == "" {
		auth.AccessKey = os.Getenv("BUILDKITE_S3_ACCESS_KEY")
	}

	auth.SecretKey = os.Getenv("BUILDKITE_S3_SECRET_ACCESS_KEY")
	if auth.SecretKey == "" {
		auth.SecretKey = os.Getenv("BUILDKITE_S3_SECRET_KEY")
	}

	if auth.AccessKey == "" {
		err = errors.New("BUILDKITE_S3_ACCESS_KEY_ID or BUILDKITE_S3_ACCESS_KEY not found in environment")
	}
	if auth.SecretKey == "" {
		err = errors.New("BUILDKITE_S3_SECRET_ACCESS_KEY or BUILDKITE_S3_SECRET_KEY not found in environment")
	}

	return
}

func awsS3Region() (region aws.Region, err error) {
	regionName := "us-east-1"
	if os.Getenv("BUILDKITE_S3_DEFAULT_REGION") != "" {
		regionName = os.Getenv("BUILDKITE_S3_DEFAULT_REGION")
	} else if os.Getenv("AWS_DEFAULT_REGION") != "" {
		regionName = os.Getenv("AWS_DEFAULT_REGION")
	}

	// Check to make sure the region exists. There is a GetRegion API, but
	// there doesn't seem to be a way to make it error out if the region
	// doesn't exist.
	region, ok := aws.Regions[regionName]
	if ok == false {
		err = errors.New("Unknown AWS S3 Region `" + regionName + "`")
	}

	return
}
