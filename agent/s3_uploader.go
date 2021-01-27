package agent

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/mime"
)

type S3Uploader struct {
	// The destination which includes the S3 bucket name and the path.
	// e.g s3://my-bucket-name/foo/bar
	Destination string

	// Whether or not HTTP calls should be debugged
	DebugHTTP bool

	// The aws s3 client
	s3Client *s3.S3
}

func (u *S3Uploader) Setup(destination string, debugHTTP bool) error {
	u.Destination = destination
	u.DebugHTTP = debugHTTP

	// Initialize the s3 client, and authenticate it
	s3Client, err := newS3Client(u.BucketName())
	if err != nil {
		return err
	}

	u.s3Client = s3Client
	return nil
}

func (u *S3Uploader) URL(artifact *api.Artifact) string {
	baseUrl := "https://" + u.BucketName() + ".s3.amazonaws.com"

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
		return fmt.Errorf("Invalid S3 ACL `%s`", permission)
	}

	// Initialize the s3 client, and authenticate it
	s3Client, err := newS3Client(u.BucketName())
	if err != nil {
		return err
	}

	// Create an uploader with the session and default options
	uploader := s3manager.NewUploaderWithClient(s3Client)

	// Open file from filesystem
	logger.Debug("Reading file \"%s\"", artifact.AbsolutePath)
	f, err := os.Open(artifact.AbsolutePath)
	if err != nil {
		return fmt.Errorf("failed to open file %q (%v)", artifact.AbsolutePath, err)
	}

	// Upload the file to S3.
	logger.Debug("Uploading \"%s\" to bucket with permission `%s`", u.artifactPath(artifact), permission)
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket:      aws.String(u.BucketName()),
		Key:         aws.String(u.artifactPath(artifact)),
		ContentType: aws.String(u.mimeType(artifact)),
		ACL:         aws.String(permission),
		Body:        f,
	})

	return err
}

func (u *S3Uploader) artifactPath(artifact *api.Artifact) string {
	parts := []string{u.BucketPath(), artifact.Path}

	return strings.Join(parts, "/")
}

func (u *S3Uploader) BucketPath() string {
	return strings.Join(u.destinationParts()[1:len(u.destinationParts())], "/")
}

func (u *S3Uploader) BucketName() string {
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
