package agent

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AdRoll/goamz/s3"
	"github.com/buildkite/agent/logger"
)

type S3Downloader struct {
	// The name of the bucket
	Bucket string

	// The root directory of the download
	Destination string

	// The relative path that should be preserved in the download folder,
	// also it's location in the bucket
	Path string

	// How many times should it retry the download before giving up
	Retries int

	// If failed responses should be dumped to the log
	DebugHTTP bool
}

func (d S3Downloader) Start() error {
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

	logger.Debug("Authorizing S3 credentials and finding bucket `%s` in region `%s`...", d.BucketName(), region.Name)

	// Find the bucket
	s3 := s3.New(auth, region)
	bucket := s3.Bucket(d.BucketName())

	// If the list doesn't return an error, then we've got our bucket
	_, err = bucket.List("", "", "", 0)
	if err != nil {
		return errors.New("Could not find bucket `" + d.BucketName() + "` in region `" + region.Name + "` (" + err.Error() + ")")
	}

	// Generate a Signed URL
	signedURL := bucket.SignedURL(d.BucketFileLocation(), time.Now().Add(time.Hour))

	// We can now cheat and pass the URL onto our regular downloader
	return Download{
		URL:         signedURL,
		Path:        d.Path,
		Destination: d.Destination,
		Retries:     d.Retries,
		DebugHTTP:   d.DebugHTTP,
	}.Start()
}

func (d S3Downloader) BucketFileLocation() string {
	if d.BucketPath() != "" {
		return strings.TrimSuffix(d.BucketPath(), "/") + "/" + strings.TrimPrefix(d.Path, "/")
	} else {
		return d.Path
	}
}

func (d S3Downloader) BucketPath() string {
	return strings.Join(d.destinationParts()[1:len(d.destinationParts())], "/")
}

func (d S3Downloader) BucketName() string {
	return d.destinationParts()[0]
}

func (d S3Downloader) destinationParts() []string {
	trimmed := strings.TrimPrefix(d.Bucket, "s3://")

	return strings.Split(trimmed, "/")
}
