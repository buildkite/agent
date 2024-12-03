package artifact

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/roko"
)

type S3UploaderConfig struct {
	// The destination which includes the S3 bucket name and the path.
	// For example, s3://my-bucket-name/foo/bar
	Destination string
}

type S3Uploader struct {
	// The s3 bucket path set from the destination
	BucketPath string

	// The s3 bucket name set from the destination
	BucketName string

	// The s3 client to use
	client *s3.S3

	// The configuration
	conf S3UploaderConfig

	// The logger instance to use
	logger logger.Logger
}

func NewS3Uploader(ctx context.Context, l logger.Logger, c S3UploaderConfig) (*S3Uploader, error) {
	bucketName, bucketPath := ParseS3Destination(c.Destination)

	r := roko.NewRetrier(
		roko.WithMaxAttempts(10),
		roko.WithStrategy(roko.Exponential(2*time.Second, 0)),
		roko.WithJitter(),
	)

	s3Client, err := roko.DoFunc(ctx, r, func(*roko.Retrier) (*s3.S3, error) {
		// Initialize the s3 client, and authenticate it
		return NewS3Client(l, bucketName)
	})

	if err != nil {
		return nil, err
	}

	return &S3Uploader{
		logger:     l,
		conf:       c,
		client:     s3Client,
		BucketName: bucketName,
		BucketPath: bucketPath,
	}, nil
}

func ParseS3Destination(destination string) (string, string) {
	destinationWithNoTrailingSlash := strings.TrimSuffix(destination, "/")
	destinationWithNoProtocol := strings.TrimPrefix(destinationWithNoTrailingSlash, "s3://")
	parts := strings.Split(destinationWithNoProtocol, "/")
	path := strings.Join(parts[1:], "/")
	bucket := parts[0]
	return bucket, path
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

func (u *S3Uploader) CreateWork(artifact *api.Artifact) ([]workUnit, error) {
	return []workUnit{&s3UploaderWork{
		S3Uploader: u,
		artifact:   artifact,
	}}, nil
}

type s3UploaderWork struct {
	*S3Uploader
	artifact *api.Artifact
}

func (u *s3UploaderWork) Artifact() *api.Artifact { return u.artifact }

func (u *s3UploaderWork) Description() string {
	return singleUnitDescription(u.artifact)
}

func (u *s3UploaderWork) DoWork(context.Context) (*api.ArtifactPartETag, error) {
	permission, err := u.resolvePermission()
	if err != nil {
		return nil, err
	}

	// Create an uploader with the session and default options
	uploader := s3manager.NewUploaderWithClient(u.client)

	// Open file from filesystem
	u.logger.Debug("Reading file %q", u.artifact.AbsolutePath)
	f, err := os.Open(u.artifact.AbsolutePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %q (%w)", u.artifact.AbsolutePath, err)
	}

	// Upload the file to S3.
	u.logger.Debug("Uploading %q to bucket with permission %q", u.artifactPath(u.artifact), permission)

	params := &s3manager.UploadInput{
		Bucket:      aws.String(u.BucketName),
		Key:         aws.String(u.artifactPath(u.artifact)),
		ContentType: aws.String(u.artifact.ContentType),
		ACL:         aws.String(permission),
		Body:        f,
	}
	// if enabled we assign the sse configuration
	if u.serverSideEncryptionEnabled() {
		params.ServerSideEncryption = aws.String("AES256")
	}

	_, err = uploader.Upload(params)
	return nil, err
}

func (u *S3Uploader) artifactPath(artifact *api.Artifact) string {
	parts := []string{u.BucketPath, artifact.Path}

	return strings.Join(parts, "/")
}

func (u *S3Uploader) resolvePermission() (string, error) {
	permission := "public-read"
	if os.Getenv("BUILDKITE_S3_ACL") != "" {
		permission = os.Getenv("BUILDKITE_S3_ACL")
	} else if os.Getenv("AWS_S3_ACL") != "" {
		permission = os.Getenv("AWS_S3_ACL")
	}

	switch permission {
	case "private", "public-read", "public-read-write", "authenticated-read", "bucket-owner-read", "bucket-owner-full-control":
		return permission, nil
	default:
		return "", fmt.Errorf("Invalid S3 ACL value: `%s`", permission)
	}
}

// is encryption at rest enabled for artifacts to satisfy basic security requirements
func (u *S3Uploader) serverSideEncryptionEnabled() bool {
	sse := os.Getenv("BUILDKITE_S3_SSE_ENABLED")
	switch {
	case strings.ToLower(sse) == "true":
		return true
	default:
		return false
	}
}
