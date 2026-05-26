package store

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithymiddleware "github.com/aws/smithy-go/middleware"
	"github.com/buildkite/agent/v3/internal/cache/internal/trace"
	"go.opentelemetry.io/otel/attribute"
)

// Options holds configuration for S3Blob and can be constructed from an S3 URL in a similar way to gocloud.dev
// Example S3 URLs:
//
//	s3://my-bucket
//	s3://my-bucket/prefix
//	s3://my-bucket?region=us-east-1
//	s3://my-bucket/prefix?region=us-east-1&endpoint=http://localhost:9000&use_path_style=true
type Options struct {
	S3Endpoint   string
	Bucket       string
	Region       string
	Prefix       string
	UsePathStyle bool
	Concurrency  int
	PartSizeMB   int
}

func OptionsFromURL(s3url string) (*Options, error) {
	u, err := url.Parse(s3url)
	if err != nil {
		return nil, fmt.Errorf("failed to parse S3 URL: %w", err)
	}

	// check the scheme is s3
	if u.Scheme != "s3" {
		return nil, fmt.Errorf("invalid S3 URL scheme %q: must be s3", u.Scheme)
	}

	opts := &Options{
		Bucket: u.Hostname(),
		Prefix: strings.Trim(u.Path, "/"),
		// Region and S3Endpoint can be set via query parameters if needed
		Region:     u.Query().Get("region"),
		S3Endpoint: u.Query().Get("endpoint"),
	}

	if opts.Region == "" {
		opts.Region = "us-east-1"
	}

	if u.Query().Get("use_path_style") == "true" {
		opts.UsePathStyle = true
	}

	if concurrencyStr := u.Query().Get("concurrency"); concurrencyStr != "" {
		concurrency, err := strconv.Atoi(concurrencyStr)
		if err != nil {
			return nil, fmt.Errorf("invalid concurrency value %q: %w", concurrencyStr, err)
		}
		if concurrency < 0 || concurrency > 100 {
			return nil, fmt.Errorf("concurrency must be between 0 and 100, got %d", concurrency)
		}
		opts.Concurrency = concurrency
	}

	if partSizeStr := u.Query().Get("part_size_mb"); partSizeStr != "" {
		partSizeMB, err := strconv.Atoi(partSizeStr)
		if err != nil {
			return nil, fmt.Errorf("invalid part_size_mb value %q: %w", partSizeStr, err)
		}
		if partSizeMB < 0 || (partSizeMB > 0 && partSizeMB < 5) || partSizeMB > 5120 {
			return nil, fmt.Errorf("part_size_mb must be 0 (default) or between 5 and 5120, got %d", partSizeMB)
		}
		opts.PartSizeMB = partSizeMB
	}

	return opts, nil
}

// S3Blob implements the Blob interface using AWS S3
type S3Blob struct {
	client      *s3.Client
	uploader    *manager.Uploader   //nolint:staticcheck // SA1019: pending migration to transfermanager
	downloader  *manager.Downloader //nolint:staticcheck // SA1019: pending migration to transfermanager
	bucketName  string
	prefix      string
	concurrency int
	partSize    int64
}

// NewS3Blob creates a new S3Blob instance using an S3 URL and prefix
func NewS3Blob(ctx context.Context, s3url string) (*S3Blob, error) {
	opts, err := OptionsFromURL(s3url)
	if err != nil {
		return nil, fmt.Errorf("failed to parse S3 URL: %w", err)
	}

	// Load the AWS configuration
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	slog.Debug("configured S3 bucket",
		"bucket", opts.Bucket,
		"region", opts.Region,
		"prefix", opts.Prefix,
		"endpoint", opts.S3Endpoint)

	// Create a new S3 client
	client := s3.NewFromConfig(cfg,
		func(o *s3.Options) {
			o.Region = opts.Region
			if opts.UsePathStyle {
				o.UsePathStyle = true
			}

			// used for local testing or custom S3 endpoints
			if opts.S3Endpoint != "" {
				o.BaseEndpoint = aws.String(opts.S3Endpoint)
			}
		})

	// Determine concurrency (default to SDK default if not specified)
	concurrency := opts.Concurrency
	if concurrency == 0 {
		concurrency = manager.DefaultUploadConcurrency
	}

	// Determine part size (default to SDK default if not specified)
	// Convert MB to bytes
	partSize := manager.DefaultUploadPartSize
	if opts.PartSizeMB > 0 {
		partSize = int64(opts.PartSizeMB) * 1024 * 1024
	}

	// Create the uploader and downloader with configured settings
	uploader := manager.NewUploader(client, func(u *manager.Uploader) { //nolint:staticcheck // SA1019: pending migration to transfermanager
		u.Concurrency = concurrency
		u.PartSize = partSize
	})
	downloader := manager.NewDownloader(client, func(d *manager.Downloader) { //nolint:staticcheck // SA1019: pending migration to transfermanager
		d.Concurrency = concurrency
		d.PartSize = partSize
	})

	slog.Debug("configured S3 transfer manager",
		"concurrency", concurrency,
		"part_size_bytes", partSize,
	)

	return &S3Blob{
		client:      client,
		uploader:    uploader,
		downloader:  downloader,
		bucketName:  opts.Bucket,
		prefix:      opts.Prefix,
		concurrency: concurrency,
		partSize:    partSize,
	}, nil
}

// Upload uploads a file to S3 using multipart upload for parallel transfers
func (b *S3Blob) Upload(ctx context.Context, filePath, key string) (*TransferInfo, error) {
	ctx, span := trace.Start(ctx, "S3Blob.Upload")
	defer span.End()

	start := time.Now()

	// Get the full key with prefix
	fullKey := b.getFullKey(key)

	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer func() {
		_ = file.Close()
	}()

	// stat the file to get its size
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file %s: %w", filePath, err)
	}

	bytesWritten := fileInfo.Size()

	slog.Debug("starting S3 upload",
		"key", fullKey,
		"file_size", bytesWritten,
		"concurrency", b.concurrency,
	)

	// Upload the file to S3 using the multipart uploader
	result, err := b.uploader.Upload(ctx, &s3.PutObjectInput{ //nolint:staticcheck // SA1019: pending migration to transfermanager
		Bucket: aws.String(b.bucketName),
		Key:    aws.String(fullKey),
		Body:   file,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upload file to S3: %w", err)
	}

	// Get actual part count from completed parts
	// For single part uploads (small files), CompletedParts is empty so default to 1
	partCount := len(result.CompletedParts)
	if partCount == 0 {
		partCount = 1
	}

	// Extract request ID from the upload result (only set for multipart uploads)
	requestID := result.UploadID

	duration := time.Since(start)
	averageSpeed := calculateTransferSpeedMBps(bytesWritten, duration)

	slog.Debug("completed S3 upload",
		"key", fullKey,
		"bytes_transferred", bytesWritten,
		"parts_uploaded", partCount,
		"concurrency", b.concurrency,
		"duration", duration,
		"transfer_speed_mbps", fmt.Sprintf("%.2f", averageSpeed),
	)

	span.SetAttributes(
		attribute.Int64("bytes_transferred", bytesWritten),
		attribute.String("transfer_speed", fmt.Sprintf("%.2fMB/s", averageSpeed)),
		attribute.String("request_id", requestID),
		attribute.Int("part_count", partCount),
		attribute.Int("concurrency", b.concurrency),
	)

	return &TransferInfo{
		BytesTransferred: bytesWritten,
		TransferSpeed:    averageSpeed,
		RequestID:        requestID,
		Duration:         duration,
		PartCount:        partCount,
		Concurrency:      b.concurrency,
	}, nil
}

// Download downloads a file from S3 using parallel range requests for large files
func (b *S3Blob) Download(ctx context.Context, key, destPath string) (*TransferInfo, error) {
	ctx, span := trace.Start(ctx, "S3Blob.Download")
	defer span.End()

	start := time.Now()

	// Get the full key with prefix
	fullKey := b.getFullKey(key)

	// Create the destination file - must support WriteAt for parallel downloads
	destFile, err := os.Create(destPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create destination file %s: %w", destPath, err)
	}
	defer func() {
		_ = destFile.Close()
	}()

	slog.Debug("starting S3 download",
		"key", fullKey,
		"concurrency", b.concurrency,
	)

	// Track number of GetObject requests (parts) made during download
	var partCount atomic.Int32

	// Download the file from S3 using parallel range requests
	bytesWritten, err := b.downloader.Download(ctx, destFile, &s3.GetObjectInput{ //nolint:staticcheck // SA1019: pending migration to transfermanager
		Bucket: aws.String(b.bucketName),
		Key:    aws.String(fullKey),
	}, func(d *manager.Downloader) { //nolint:staticcheck // SA1019: pending migration to transfermanager
		d.ClientOptions = append(d.ClientOptions, func(o *s3.Options) {
			o.APIOptions = append(o.APIOptions, func(stack *smithymiddleware.Stack) error {
				return stack.Initialize.Add(smithymiddleware.InitializeMiddlewareFunc(
					"PartCounter",
					func(ctx context.Context, in smithymiddleware.InitializeInput, next smithymiddleware.InitializeHandler) (smithymiddleware.InitializeOutput, smithymiddleware.Metadata, error) {
						partCount.Add(1)
						return next.HandleInitialize(ctx, in)
					},
				), smithymiddleware.Before)
			})
		})
	})
	if err != nil {
		return nil, fmt.Errorf("failed to download file from S3: %w", err)
	}

	// Get actual part count from interceptor
	actualPartCount := int(partCount.Load())
	if actualPartCount == 0 {
		actualPartCount = 1
	}

	duration := time.Since(start)
	averageSpeed := calculateTransferSpeedMBps(bytesWritten, duration)

	slog.Debug("completed S3 download",
		"key", fullKey,
		"bytes_transferred", bytesWritten,
		"parts_downloaded", actualPartCount,
		"concurrency", b.concurrency,
		"duration", duration,
		"transfer_speed_mbps", fmt.Sprintf("%.2f", averageSpeed),
	)

	span.SetAttributes(
		attribute.Int64("bytes_transferred", bytesWritten),
		attribute.String("transfer_speed", fmt.Sprintf("%.2fMB/s", averageSpeed)),
		attribute.Int("part_count", actualPartCount),
		attribute.Int("concurrency", b.concurrency),
	)

	// Copy the object to itself to reset the LastModified timestamp,
	// which extends the lifecycle expiration.
	copySource := fmt.Sprintf("%s/%s", b.bucketName, fullKey)
	_, err = b.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:            aws.String(b.bucketName),
		Key:               aws.String(fullKey),
		CopySource:        aws.String(copySource),
		MetadataDirective: "REPLACE",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to refresh object expiration: %w", err)
	}

	slog.Debug("refreshed object expiration",
		"key", fullKey,
		"bucket", b.bucketName,
	)

	return &TransferInfo{
		BytesTransferred: bytesWritten,
		TransferSpeed:    averageSpeed,
		RequestID:        "", // Download doesn't return a single request ID for parallel downloads
		Duration:         duration,
		PartCount:        actualPartCount,
		Concurrency:      b.concurrency,
	}, nil
}

// getFullKey combines the prefix with the key
func (b *S3Blob) getFullKey(key string) string {
	// Remove leading slash from key if present
	key = strings.TrimPrefix(key, "/")
	// Combine prefix and key
	return path.Join(b.prefix, key)
}
