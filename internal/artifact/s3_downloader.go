package artifact

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/buildkite/agent/v3/internal/agenthttp"
	"github.com/buildkite/agent/v3/logger"
)

type S3DownloaderConfig struct {
	// The client for interacting with S3
	S3Client *s3.Client

	// The S3 bucket name and the path, for example, s3://my-bucket-name/foo/bar
	S3Path string

	// The root directory of the download
	Destination string

	// The relative path that should be preserved in the download folder,
	// also its location in the bucket
	Path string

	// How many times should it retry the download before giving up
	Retries int

	// If failed responses should be dumped to the log
	DebugHTTP    bool
	TraceHTTP    bool
	DisableHTTP2 bool
}

type S3Downloader struct {
	// The download config
	conf S3DownloaderConfig

	// The logger instance to use
	logger logger.Logger
}

func NewS3Downloader(l logger.Logger, c S3DownloaderConfig) *S3Downloader {
	return &S3Downloader{
		conf:   c,
		logger: l,
	}
}

func (d S3Downloader) Start(ctx context.Context) error {
	if d.conf.S3Client == nil {
		return fmt.Errorf("s3Downloader for %s: S3Client is nil", d.conf.S3Path)
	}

	presigner := s3.NewPresignClient(d.conf.S3Client)

	req, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(d.BucketName()),
		Key:    aws.String(d.BucketFileLocation()),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = time.Duration(time.Hour)
	})
	if err != nil {
		return fmt.Errorf("error pre-signing request: %w", err)
	}

	// We can now cheat and pass the URL onto our regular downloader
	client := agenthttp.NewClient(
		agenthttp.WithAllowHTTP2(!d.conf.DisableHTTP2),
		agenthttp.WithNoTimeout,
	)
	return NewDownload(d.logger, client, DownloadConfig{
		URL:         req.URL,
		Headers:     req.SignedHeader,
		Method:      req.Method,
		Path:        d.conf.Path,
		Destination: d.conf.Destination,
		Retries:     d.conf.Retries,
		DebugHTTP:   d.conf.DebugHTTP,
		TraceHTTP:   d.conf.TraceHTTP,
	}).Start(ctx)
}

func (d S3Downloader) BucketFileLocation() string {
	if d.BucketPath() != "" {
		return strings.TrimSuffix(d.BucketPath(), "/") + "/" + strings.TrimPrefix(d.conf.Path, "/")
	} else {
		return d.conf.Path
	}
}

func (d S3Downloader) BucketPath() string {
	return strings.Join(d.destinationParts()[1:len(d.destinationParts())], "/")
}

func (d S3Downloader) BucketName() string {
	return d.destinationParts()[0]
}

func (d S3Downloader) destinationParts() []string {
	trimmed := strings.TrimPrefix(d.conf.S3Path, "s3://")

	return strings.Split(trimmed, "/")
}
