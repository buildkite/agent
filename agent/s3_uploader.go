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

type S3UploaderConfig struct {
	// The destination which includes the S3 bucket name and the path.
	// e.g s3://my-bucket-name/foo/bar
	Destination string

	// Whether or not HTTP calls should be debugged
	DebugHTTP bool
}

type S3Uploader struct {
	// The s3 bucket path set from the destination
	BucketPath string

	// The s3 bucket name set from the destination
	BucketName string

	// The configuration
	conf S3UploaderConfig

	// The logger instance to use
	logger *logger.Logger

	// The aws s3 client
	s3Client *s3.S3
}

func NewS3Uploader(l *logger.Logger, c S3UploaderConfig) (*S3Uploader, error) {
	bucketName, bucketPath := parseS3Destination(c.Destination)

	// Initialize the s3 client, and authenticate it
	s3Client, err := newS3Client(l, bucketName)
	if err != nil {
		return nil, err
	}

	return &S3Uploader{
		logger: l,
		conf: c,
		s3Client: s3Client,
		BucketName: bucketName,
		BucketPath: bucketPath,
	}, nil
}

func parseS3Destination(destination string) (name string, path string) {
	parts := strings.Split(strings.TrimPrefix(string(destination), "s3://"), "/")
	path = strings.Join(parts[1:len(parts)], "/")
	name = parts[0]
	return
}

func (u *S3Uploader) URL(artifact *api.Artifact) string {
	baseUrl := "https://" + u.BucketName + ".s3.amazonaws.com"

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
	s3Client, err := newS3Client(u.logger, u.BucketName)
	if err != nil {
		return err
	}

	// Create an uploader with the session and default options
	uploader := s3manager.NewUploaderWithClient(s3Client)

	// Open file from filesystem
	u.logger.Debug("Reading file \"%s\"", artifact.AbsolutePath)
	f, err := os.Open(artifact.AbsolutePath)
	if err != nil {
		return fmt.Errorf("failed to open file %q (%v)", artifact.AbsolutePath, err)
	}

	// Upload the file to S3.
	u.logger.Debug("Uploading \"%s\" to bucket with permission `%s`", u.artifactPath(artifact), permission)
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket:      aws.String(u.BucketName),
		Key:         aws.String(u.artifactPath(artifact)),
		ContentType: aws.String(u.mimeType(artifact)),
		ACL:         aws.String(permission),
		Body:        f,
	})

	return err
}

func (u *S3Uploader) artifactPath(artifact *api.Artifact) string {
	parts := []string{u.BucketPath, artifact.Path}

	return strings.Join(parts, "/")
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
