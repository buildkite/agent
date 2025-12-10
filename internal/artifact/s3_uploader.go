package artifact

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
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
	client *s3.Client

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

	s3Client, err := roko.DoFunc(ctx, r, func(*roko.Retrier) (*s3.Client, error) {
		// Initialize the s3 client, and authenticate it
		return NewS3Client(ctx, l, bucketName)
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

	uri, _ := url.Parse(baseUrl)
	uri.Path = path.Join(uri.Path, u.artifactPath(artifact))

	return uri.String()
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

func (u *s3UploaderWork) DoWork(ctx context.Context) (*api.ArtifactPartETag, error) {
	permission, err := u.resolvePermission()
	if err != nil {
		return nil, err
	}

	// Create an uploader with the session and default options
	uploader := manager.NewUploader(u.client)

	// Open file from filesystem
	u.logger.Debug("Reading file %q", u.artifact.AbsolutePath)
	f, err := os.Open(u.artifact.AbsolutePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %q (%w)", u.artifact.AbsolutePath, err)
	}

	// Upload the file to S3.
	u.logger.Debug("Uploading %q to bucket with permission %q", u.artifactPath(u.artifact), permission)

	params := &s3.PutObjectInput{
		Bucket:      aws.String(u.BucketName),
		Key:         aws.String(u.artifactPath(u.artifact)),
		ContentType: aws.String(u.artifact.ContentType),
		ACL:         permission,
		Body:        f,
	}

	// if enabled we assign the sse configuration
	if u.serverSideEncryptionEnabled() {
		params.ServerSideEncryption = types.ServerSideEncryptionAes256
	}

	_, err = uploader.Upload(ctx, params)
	return nil, err
}

func (u *S3Uploader) artifactPath(artifact *api.Artifact) string {
	if u.BucketPath == "" {
		return artifact.Path
	}

	return path.Join(u.BucketPath, artifact.Path)
}

func (u *S3Uploader) resolvePermission() (types.ObjectCannedACL, error) {
	permission := types.ObjectCannedACLPublicRead
	switch {
	case os.Getenv("BUILDKITE_S3_ACL") != "":
		permission = types.ObjectCannedACL(os.Getenv("BUILDKITE_S3_ACL"))
	case os.Getenv("AWS_S3_ACL") != "":
		permission = types.ObjectCannedACL(os.Getenv("AWS_S3_ACL"))
	}

	if !slices.Contains(permission.Values(), permission) {
		return "", invalidACLError(permission)
	}
	return permission, nil
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

type invalidACLError string

func (e invalidACLError) Error() string {
	return fmt.Sprintf("invalid S3 ACL value: %q", string(e))
}
