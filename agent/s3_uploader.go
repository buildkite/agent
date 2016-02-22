package agent

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/AdRoll/goamz/s3"
	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/mime"
)

type S3Uploader struct {
	// The destination which includes the S3 bucket name
	// and the path.
	// s3://my-bucket-name/foo/bar
	Destination string

	// Whether or not HTTP calls shoud be debugged
	DebugHTTP bool

	// The S3 Bucket we're uploading these files to
	Bucket *s3.Bucket
}

func (u *S3Uploader) Setup(destination string, debugHTTP bool) error {
	u.Destination = destination
	u.DebugHTTP = debugHTTP

	// Try to auth with S3
	auth, err := awsS3Auth()
	if err != nil {
		return errors.New(fmt.Sprintf("Error creating AWS S3 authentication: %s", err.Error()))
	}

	// Try and get the region
	region, err := awsS3Region()
	if err != nil {
		return err
	}

	logger.Debug("Authorizing S3 credentials and finding bucket `%s` in region `%s`...", u.bucketName(), region.Name)

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

func (u *S3Uploader) URL(artifact *api.Artifact) string {
	baseUrl := "http://" + u.bucketName() + ".s3.amazonaws.com"
	if os.Getenv("BUILDKITE_S3_ACCESS_URL") != "" {
		baseUrl = os.Getenv("BUILDKITE_S3_ACCESS_URL")
	}
	url, _ := url.Parse(baseUrl)

	url.Path += u.artifactPath(artifact)

	return url.String()
}

func (u *S3Uploader) Upload(artifact *api.Artifact) error {
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
	err = u.Bucket.Put(u.artifactPath(artifact), data, u.mimeType(artifact), Perms, s3.Options{})
	if err != nil {
		return errors.New(fmt.Sprintf("Failed to PUT file \"%s\" (%s)", u.artifactPath(artifact), err.Error()))
	}

	return nil
}

func (u *S3Uploader) artifactPath(artifact *api.Artifact) string {
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
	trimmed := strings.TrimPrefix(u.Destination, "s3://")

	return strings.Split(trimmed, "/")
}

func (u *S3Uploader) mimeType(a *api.Artifact) string {
	extension := filepath.Ext(a.Path)
	mimeType := mime.TypeByExtension(extension)

	if mimeType != "" {
		return mimeType
	} else {
		return "binary/octet-stream"
	}
}
