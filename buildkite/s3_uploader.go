package buildkite

import (
	"errors"
	"fmt"
	"github.com/AdRoll/goamz/aws"
	"github.com/AdRoll/goamz/s3"
	"github.com/buildkite/agent/buildkite/logger"
	"io/ioutil"
	"net/url"
	"os"
	"strings"
	"time"
)

type S3Uploader struct {
	// The destination which includes the S3 bucket name
	// and the path.
	// s3://my-bucket-name/foo/bar
	Destination string

	// The S3 Bucket we're uploading these files to
	Bucket *s3.Bucket
}

func (u *S3Uploader) Setup(destination string) error {
	u.Destination = destination

	// Try to auth with S3
	auth, err := awsS3Auth()
	if err != nil {
		return errors.New(fmt.Sprintf("Error creating AWS S3 authentication: %s", err.Error()))
	}

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
		return errors.New("Unknown AWS S3 Region `" + regionName + "`")
	}

	logger.Debug("Authorizing S3 credentials and finding bucket `%s` in region `%s`...", u.bucketName(), regionName)

	// Find the bucket
	s3 := s3.New(auth, region)
	bucket := s3.Bucket(u.bucketName())

	// If the list doesn't return an error, then we've got our bucket
	_, err = bucket.List("", "", "", 0)
	if err != nil {
		return errors.New("Could not find bucket `" + u.bucketName() + "` in region `" + region.Name + "` (" + err.Error() + ")")
	}

	u.Bucket = bucket

	return nil
}

func (u *S3Uploader) URL(artifact *Artifact) string {
	url, _ := url.Parse("http://" + u.bucketName() + ".s3.amazonaws.com")

	url.Path += u.artifactPath(artifact)

	return url.String()
}

func (u *S3Uploader) Upload(artifact *Artifact) error {
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

	Perms := s3.ACL(permission)

	logger.Debug("Reading file \"%s\"", artifact.AbsolutePath)
	data, err := ioutil.ReadFile(artifact.AbsolutePath)
	if err != nil {
		return errors.New("Failed to read file " + artifact.AbsolutePath + " (" + err.Error() + ")")
	}

	logger.Debug("Uploading \"%s\" to bucket with permission `%s`", u.artifactPath(artifact), permission)
	err = u.Bucket.Put(u.artifactPath(artifact), data, artifact.MimeType(), Perms, s3.Options{})
	if err != nil {
		return errors.New(fmt.Sprintf("Failed to PUT file \"%s\" (%s)", u.artifactPath(artifact), err.Error()))
	}

	return nil
}

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

func (u *S3Uploader) artifactPath(artifact *Artifact) string {
	parts := []string{u.bucketPath(), artifact.Path}

	return strings.Join(parts, "/")
}

func (u *S3Uploader) bucketPath() string {
	return strings.Join(u.destinationParts()[1:len(u.destinationParts())], "/")
}

func (u *S3Uploader) bucketName() string {
	return u.destinationParts()[0]
}

func (u *S3Uploader) destinationParts() []string {
	trimmed_string := strings.TrimLeft(u.Destination, "s3://")

	return strings.Split(trimmed_string, "/")
}
