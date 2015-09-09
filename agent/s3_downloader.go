package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/buildkite/agent/logger"
)

var (
	globalS3DownloaderManagerByBucket = map[string]*s3manager.Downloader{}
	globalS3Mutex                     sync.Mutex
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

	// The S3 download manager
	Downloader *s3manager.Downloader
}

func (d S3Downloader) Start() error {
	var downloader *s3manager.Downloader

	// Split apart the bucket
	bucketParts := strings.Split(strings.TrimLeft(d.Bucket, "s3://"), "/")
	bucketName := bucketParts[0]
	bucketPath := strings.Join(bucketParts[1:len(bucketParts)], "/")

	// Lock the global s3 bucket cache using a mutex. We do this because if
	// multiple threads are all using the s3 downloader, they may all start
	// at the same time, and all try and populate the cache at the same
	// time.
	globalS3Mutex.Lock()

	if globalS3DownloaderManagerByBucket[bucketName] == nil {
		// Initialize the s3 client, and authenticate it. Once it's
		// successfully auths, create a downloader and store it in the
		// global cache.
		s3Client, err := newS3Client(bucketName)
		if err != nil {
			return err
		}

		downloader = s3manager.NewDownloader(&s3manager.DownloadOptions{S3: s3Client})
		globalS3DownloaderManagerByBucket[bucketName] = downloader
	} else {
		downloader = globalS3DownloaderManagerByBucket[bucketName]
	}

	// Release the mutex now we've got the downloader
	globalS3Mutex.Unlock()

	// Create the location of the file
	var s3Location string
	if bucketPath != "" {
		s3Location = strings.TrimRight(bucketPath, "/") + "/" + strings.TrimLeft(d.Path, "/")
	} else {
		s3Location = d.Path
	}

	targetFile := filepath.Join(d.Destination, d.Path)
	targetDirectory, _ := filepath.Split(targetFile)

	// Ensure we have a folder to download into
	err := os.MkdirAll(targetDirectory, 0777)
	if err != nil {
		return fmt.Errorf("Failed to create folder for %s (%T: %v)", targetFile, err, err)
	}

	logger.Debug("Downloading %s/%s to %s", d.Bucket, s3Location, targetFile)

	logger.Debug("%s", d.Destination)
	logger.Debug("%s", d.Destination)
	logger.Debug("%s", downloader)
	logger.Debug("%s", s3Location)
	logger.Debug("%s", d.Path)
	logger.Debug("---")

	// // We can now cheat and pass the URL onto our regular downloader
	// return Download{
	// 	URL:         signedURL,
	// 	Path:        d.Path,
	// 	Destination: d.Destination,
	// 	Retries:     d.Retries,
	// 	DebugHTTP:   d.DebugHTTP,
	// }.Start()

	return nil
}
