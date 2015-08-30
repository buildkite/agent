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
	// The destination which includes the S3 bucket name
	// and the path.
	// s3://my-bucket-name/foo/bar
	Destination string

	// Whether or not HTTP calls shoud be debugged
	DebugHTTP bool

	Uploader *s3manager.Uploader

	// The S3 Bucket we're uploading these files to
	// Bucket *s3.Bucket
}

func (u *S3Uploader) Setup(destination string, debugHTTP bool) error {
	u.Destination = destination

	// Generate the AWS config used by the S3 client
	region := awsS3RegionFromEnv()
	config := &aws.Config{Credentials: awsS3Credentials(), Region: aws.String(region)}

	logger.Debug("Authorizing S3 credentials and finding bucket `%s` in region `%s`...", u.bucketName(), region)

	// Create the S3 client
	s3client := s3.New(config)

	// Test the authentication by trying to list the first 0 objects in the
	// bucket.
	params := &s3.ListObjectsInput{
		Bucket:  aws.String(u.bucketName()),
		MaxKeys: aws.Int64(0),
	}
	_, err := s3client.ListObjects(params)
	if err != nil {
		return fmt.Errorf("Failed to authenticate to bucket `%s` in region `%s` (s)", u.bucketName(), region, err.Error())
	}

	u.Uploader = s3manager.NewUploader(&s3manager.UploadOptions{S3: s3client})

	return nil
}

func (u *S3Uploader) URL(artifact *api.Artifact) string {
	url, _ := url.Parse("http://" + u.bucketName() + ".s3.amazonaws.com")

	url.Path += u.artifactPath(artifact)

	return url.String()
}

func (u *S3Uploader) Upload(artifact *api.Artifact) error {
	logger.Debug("Opening file \"%s\"", artifact.AbsolutePath)

	// Open the file (but don't read it's contents into memory)
	file, err := os.Open(artifact.AbsolutePath)
	if err != nil {
		return fmt.Errorf("Failed to open file \"%s\" (%s)", artifact.AbsolutePath, err.Error())
	}
	defer file.Close()

	// Construct the file upload options
	uploadInput := &s3manager.UploadInput{
		Bucket:      aws.String(u.bucketName()),
		Key:         aws.String(u.artifactPath(artifact)),
		ACL:         aws.String(awsS3PermissionFromEnv()),
		ContentType: aws.String(u.mimeType(artifact)),
		Body:        file,
	}

	logger.Debug("Uploading \"%s\" to bucket with permission `%s`", u.artifactPath(artifact), &uploadInput.ACL)

	// Now upload the file
	result, err := u.Uploader.Upload(uploadInput)
	if err != nil {
		return fmt.Errorf("Failed to upload file \"%s\" (%s)", u.artifactPath(artifact), err.Error())
	}

	logger.Debug("Successfully uploaded to: %s", result.Location)

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
	trimmed := strings.TrimLeft(u.Destination, "s3://")

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
