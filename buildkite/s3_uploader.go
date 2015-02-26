package buildkite

import (
	"errors"
	"fmt"
	"github.com/AdRoll/goamz/aws"
	"github.com/AdRoll/goamz/s3"
	"github.com/buildkite/agent/buildkite/logger"
	"io/ioutil"
	"os"
	"strings"
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

	auth := aws.Auth{}

	// Create an auth based on ENV variables. We support both
	// BUILDKITE_S3_* and the standard AWS_*
	//
	// If neither auth can be found, then we try and show the most relevant
	// error message.
	buildkiteAuth, buildkiteAuthError := createAuth("BUILDKITE_S3_")
	if buildkiteAuthError != nil {
		awsAuth, awsErr := createAuth("AWS_")
		if awsErr != nil {
			// Was there an attempt at using the AWS_ keys? If so,
			// show the error message from that attempt.
			if awsAuth.AccessKey != "" || awsAuth.SecretKey != "" {
				return errors.New(fmt.Sprintf("Error creating AWS S3 authentication: %s", awsErr.Error()))
			} else {
				return errors.New(fmt.Sprintf("Error creating AWS S3 authentication: %s", buildkiteAuthError.Error()))
			}
		} else {
			auth = awsAuth
		}
	} else {
		auth = buildkiteAuth
	}

	// Decide what region to use. I think S3 defaults to us-east-1
	// https://github.com/AdRoll/goamz/blob/master/aws/regions.go
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
	_, err := bucket.List("", "", "", 0)
	if err != nil {
		return errors.New("Could not find bucket `" + u.bucketName() + "` in region `" + region.Name + "` (" + err.Error() + ")")
	}

	u.Bucket = bucket

	return nil
}

func (u *S3Uploader) URL(artifact *Artifact) string {
	return "http://" + u.bucketName() + ".s3.amazonaws.com/" + u.artifactPath(artifact)
}

func (u *S3Uploader) Upload(artifact *Artifact) error {
	// Define the permission to use. Allow override by an ENV variable
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

// Create an AWS authentication based on environment variables.
func createAuth(prefix string) (aws.Auth, error) {
	auth := aws.Auth{}

	// Get the access key. Support ACCESS_KEY_ID or just ACCESS_KEY
	auth.AccessKey = os.Getenv(prefix + "ACCESS_KEY_ID")
	if auth.AccessKey == "" {
		auth.AccessKey = os.Getenv(prefix + "ACCESS_KEY")
	}

	// Get the secret key. Support SECRET_ACCESS_KEY or just SECRET_KEY
	auth.SecretKey = os.Getenv(prefix + "SECRET_ACCESS_KEY")
	if auth.SecretKey == "" {
		auth.SecretKey = os.Getenv(prefix + "SECRET_KEY")
	}

	// No auth key?
	if auth.AccessKey == "" {
		return auth, errors.New(fmt.Sprintf("%sACCESS_KEY_ID or %sACCESS_KEY not found in environment", prefix, prefix))
	}

	// No secret key?
	if auth.SecretKey == "" {
		return auth, errors.New(fmt.Sprintf("%sSECRET_ACCESS_KEY or %sSECRET_KEY not found in environment", prefix, prefix))
	}

	return auth, nil
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
