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

	// Passing blank values here instructs the AWS library to look at the
	// current instances meta data for the security credentials.
	auth, err := aws.GetAuth("", "", "", time.Time{})
	if err != nil {
		return errors.New(fmt.Sprintf("Error creating AWS authentication: %s", err.Error()))
	}

	// Decide what region to use
	// https://github.com/AdRoll/goamz/blob/master/aws/regions.go
	// I think S3 defaults to us-east-1
	regionName := "us-east-1"
	if os.Getenv("AWS_DEFAULT_REGION") != "" {
		regionName = os.Getenv("AWS_DEFAULT_REGION")
	}

	// Check to make sure the region exists
	region, ok := aws.Regions[regionName]
	if ok == false {
		return errors.New("Unknown AWS Region `" + regionName + "`")
	}

	// Find the bucket
	s3 := s3.New(auth, region)
	bucket := s3.Bucket(u.bucketName())

	// If the list doesn't return an error, then we've got our
	// bucket
	_, err = bucket.List("", "", "", 0)
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
	if os.Getenv("AWS_S3_ACL") != "" {
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

	logger.Debug("Reading file %s", artifact.AbsolutePath)
	data, err := ioutil.ReadFile(artifact.AbsolutePath)
	if err != nil {
		return errors.New("Failed to read file " + artifact.AbsolutePath + " (" + err.Error() + ")")
	}

	logger.Debug("Putting to %s with permission %s", u.artifactPath(artifact), permission)
	err = u.Bucket.Put(u.artifactPath(artifact), data, artifact.MimeType(), Perms, s3.Options{})
	if err != nil {
		return errors.New("Failed to PUT file " + u.artifactPath(artifact) + " (" + err.Error() + ")")
	}

	return nil
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
