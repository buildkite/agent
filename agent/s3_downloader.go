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

	// Split apart the bucket
	bucketParts := strings.Split(strings.TrimLeft(d.Bucket, "s3://"), "/")
	bucketName := bucketParts[0]
	bucketPath := strings.Join(bucketParts[1:len(bucketParts)], "/")

	logger.Debug("Authorizing S3 credentials and finding bucket `%s` in region `%s`...", bucketName, region.Name)

	// Find the bucket
	s3 := s3.New(auth, region)
	bucket := s3.Bucket(bucketName)

	// If the list doesn't return an error, then we've got our bucket
	_, err = bucket.List("", "", "", 0)
	if err != nil {
		return errors.New("Could not find bucket `" + bucketName + "` in region `" + region.Name + "` (" + err.Error() + ")")
	}

	// Create the location of the file
	var s3Location string
	if bucketPath != "" {
		s3Location = strings.TrimRight(bucketPath, "/") + "/" + strings.TrimLeft(d.Path, "/")
	} else {
		s3Location = d.Path
	}

	// Generate a Signed URL
	signedURL := bucket.SignedURL(s3Location, time.Now().Add(time.Hour))

	// We can now cheat and pass the URL onto our regular downloader
	return Download{
		URL:         signedURL,
		Path:        d.Path,
		Destination: d.Destination,
		Retries:     d.Retries,
		DebugHTTP:   d.DebugHTTP,
	}.Start()
}
