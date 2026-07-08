package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithy "github.com/aws/smithy-go"
	smithymiddleware "github.com/aws/smithy-go/middleware"
	"github.com/buildkite/agent/v3/internal/cache/internal/trace"
	"github.com/buildkite/roko"
	"go.opentelemetry.io/otel/attribute"
)

// Download defaults tuned for restore performance. Restore is dominated by
// many parallel range requests, so it benefits from far more concurrency and
// larger parts than the SDK's upload-oriented defaults (5 streams x 5 MB).
// Benchmarks showed c32/p32 reaching ~757 MB/s vs ~179 MB/s at the defaults.
const (
	defaultDownloadConcurrency = 32
	defaultDownloadPartSizeMB  = 32
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

// objectDownloader is the subset of manager.Downloader used by downloadWithRetry,
// declared so the retry loop can be tested with a fake.
type objectDownloader interface {
	Download(ctx context.Context, w io.WriterAt, input *s3.GetObjectInput, options ...func(*manager.Downloader)) (int64, error) //nolint:staticcheck // SA1019: pending migration to transfermanager
}

// isPreconditionFailed returns true when an error is an S3 412 PreconditionFailed.
// This happens when:
//
//  1. The ETag of an object changes mid-restore (e.g. A TTL-refresh CopyObject by
//     a concurrent restore invalidates the If-Match guard).
//     This presents as an *awshttp.ResponseError with HTTP status code 412.
//
//  2. The CopySourceIfUnmodifiedSince precondition was not met when performing a
//     self-CopyObject, indicating that the object was modified too recently.
//     This presents as a smithy.APIError with code "PreconditionFailed".
func isPreconditionFailed(err error) bool {
	var respErr *awshttp.ResponseError
	if errors.As(err, &respErr) {
		return respErr.HTTPStatusCode() == http.StatusPreconditionFailed
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode() == "PreconditionFailed"
	}
	return false
}

// isNotFound reports whether err indicates the S3 object does not exist.
func isNotFound(err error) bool {
	var nsk *types.NoSuchKey
	return errors.As(err, &nsk)
}

// downloadWithRetry runs the multipart download, retrying on S3 412
// PreconditionFailed (a concurrent restore's TTL-refresh CopyObject changed the
// object's ETag and invalidated the SDK's If-Match guard). Returns bytes written.
func downloadWithRetry(ctx context.Context, r *roko.Retrier, d objectDownloader, destPath string, in *s3.GetObjectInput, opts ...func(*manager.Downloader)) (int64, error) { //nolint:staticcheck // SA1019: pending migration to transfermanager
	var bytesWritten int64
	err := r.DoWithContext(ctx, func(r *roko.Retrier) error {
		destFile, err := os.Create(destPath)
		if err != nil {
			r.Break()
			return fmt.Errorf("failed to create destination file %s: %w", destPath, err)
		}
		defer func() { _ = destFile.Close() }()

		n, err := d.Download(ctx, destFile, in, opts...)
		if err != nil {
			if isPreconditionFailed(err) {
				slog.Warn("cache download hit 412 (concurrent ETag change), retrying",
					"key", aws.ToString(in.Key), "retrier", r.String())
				return err // retryable
			}
			r.Break()
			return err
		}

		bytesWritten = n
		return nil
	})
	return bytesWritten, err
}

// S3Blob implements the Blob interface using AWS S3
type S3Blob struct {
	client              *s3.Client
	uploader            *manager.Uploader   //nolint:staticcheck // SA1019: pending migration to transfermanager
	downloader          *manager.Downloader //nolint:staticcheck // SA1019: pending migration to transfermanager
	bucketName          string
	prefix              string
	uploadConcurrency   int
	downloadConcurrency int
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

	settings := resolveTransferSettings(opts)

	// Keep at least as many idle connections warm per host as the highest
	// concurrency we use, so parallel parts reuse connections instead of
	// re-establishing them (the SDK default of 10 throttles higher concurrency).
	httpClient := awshttp.NewBuildableClient().WithTransportOptions(func(t *http.Transport) {
		t.MaxIdleConnsPerHost = settings.maxIdleConnsPerHost
		if t.MaxIdleConns < settings.maxIdleConnsPerHost {
			t.MaxIdleConns = settings.maxIdleConnsPerHost
		}
	})

	// Create a new S3 client
	client := s3.NewFromConfig(cfg,
		func(o *s3.Options) {
			o.Region = opts.Region
			o.HTTPClient = httpClient
			if opts.UsePathStyle {
				o.UsePathStyle = true
			}

			// used for local testing or custom S3 endpoints
			if opts.S3Endpoint != "" {
				o.BaseEndpoint = aws.String(opts.S3Endpoint)
			}
		})

	// Create the uploader and downloader with their resolved settings
	uploader := manager.NewUploader(client, func(u *manager.Uploader) { //nolint:staticcheck // SA1019: pending migration to transfermanager
		u.Concurrency = settings.uploadConcurrency
		u.PartSize = settings.uploadPartSize
	})
	downloader := manager.NewDownloader(client, func(d *manager.Downloader) { //nolint:staticcheck // SA1019: pending migration to transfermanager
		d.Concurrency = settings.downloadConcurrency
		d.PartSize = settings.downloadPartSize
	})

	slog.Debug("configured S3 transfer manager",
		"upload_concurrency", settings.uploadConcurrency,
		"upload_part_size_bytes", settings.uploadPartSize,
		"download_concurrency", settings.downloadConcurrency,
		"download_part_size_bytes", settings.downloadPartSize,
		"max_idle_conns_per_host", settings.maxIdleConnsPerHost,
	)

	return &S3Blob{
		client:              client,
		uploader:            uploader,
		downloader:          downloader,
		bucketName:          opts.Bucket,
		prefix:              opts.Prefix,
		uploadConcurrency:   settings.uploadConcurrency,
		downloadConcurrency: settings.downloadConcurrency,
	}, nil
}

// transferSettings holds the resolved concurrency and part sizes for uploads
// and downloads, plus the connection-pool size that supports them.
type transferSettings struct {
	uploadConcurrency   int
	uploadPartSize      int64
	downloadConcurrency int
	downloadPartSize    int64
	maxIdleConnsPerHost int
}

// resolveTransferSettings turns parsed Options into concrete transfer settings.
// An explicit concurrency or part_size_mb override applies to both uploads and
// downloads; otherwise uploads use the SDK's upload defaults and downloads use
// the download-tuned defaults.
func resolveTransferSettings(opts *Options) transferSettings {
	uploadConcurrency := manager.DefaultUploadConcurrency
	downloadConcurrency := defaultDownloadConcurrency
	if opts.Concurrency > 0 {
		uploadConcurrency = opts.Concurrency
		downloadConcurrency = opts.Concurrency
	}

	uploadPartSize := manager.DefaultUploadPartSize
	downloadPartSize := int64(defaultDownloadPartSizeMB) * 1024 * 1024
	if opts.PartSizeMB > 0 {
		uploadPartSize = int64(opts.PartSizeMB) * 1024 * 1024
		downloadPartSize = int64(opts.PartSizeMB) * 1024 * 1024
	}

	return transferSettings{
		uploadConcurrency:   uploadConcurrency,
		uploadPartSize:      uploadPartSize,
		downloadConcurrency: downloadConcurrency,
		downloadPartSize:    downloadPartSize,
		maxIdleConnsPerHost: max(uploadConcurrency, downloadConcurrency),
	}
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
		"concurrency", b.uploadConcurrency,
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
		"concurrency", b.uploadConcurrency,
		"duration", duration,
		"transfer_speed_mbps", fmt.Sprintf("%.2f", averageSpeed),
	)

	span.SetAttributes(
		attribute.Int64("bytes_transferred", bytesWritten),
		attribute.String("transfer_speed", fmt.Sprintf("%.2fMB/s", averageSpeed)),
		attribute.String("request_id", requestID),
		attribute.Int("part_count", partCount),
		attribute.Int("concurrency", b.uploadConcurrency),
	)

	return &TransferInfo{
		BytesTransferred: bytesWritten,
		TransferSpeed:    averageSpeed,
		RequestID:        requestID,
		Duration:         duration,
		PartCount:        partCount,
		Concurrency:      b.uploadConcurrency,
	}, nil
}

// restoreRefreshMinInterval is the minimum time-since-LastModified
// before a restore operation extends a cache object's effective TTL
// by refreshing its LastModified timestamp with a self-CopyObject operation.
//
// Objects modified within this interval are left untouched using the
// S3 CopySourceIfUnmodifiedSince precondition so that a heavily-restored
// cache object is refreshed at most once per interval.
const restoreRefreshMinInterval = 12 * time.Hour

// Download downloads a file from S3 using parallel range requests for large files
func (b *S3Blob) Download(ctx context.Context, key, destPath string) (*TransferInfo, error) {
	ctx, span := trace.Start(ctx, "S3Blob.Download")
	defer span.End()

	start := time.Now()

	// Get the full key with prefix
	fullKey := b.getFullKey(key)

	slog.Debug("starting S3 download",
		"key", fullKey,
		"concurrency", b.downloadConcurrency,
	)

	var partCount atomic.Int32

	// Download the file from S3 using parallel range requests, retrying on a
	// 412 PreconditionFailed caused by a concurrent restore's ETag change.
	retrier := roko.NewRetrier(
		roko.WithMaxAttempts(3),
		roko.WithStrategy(roko.ExponentialSubsecond(200*time.Millisecond)),
		roko.WithJitterRange(0, 250*time.Millisecond),
	)
	bytesWritten, err := downloadWithRetry(ctx, retrier, b.downloader, destPath, &s3.GetObjectInput{
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
		if isNotFound(err) {
			return nil, fmt.Errorf("%w: s3 key %s: %w", ErrBlobNotFound, fullKey, err)
		}
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
		"concurrency", b.downloadConcurrency,
		"duration", duration,
		"transfer_speed_mbps", fmt.Sprintf("%.2f", averageSpeed),
	)

	span.SetAttributes(
		attribute.Int64("bytes_transferred", bytesWritten),
		attribute.String("transfer_speed", fmt.Sprintf("%.2fMB/s", averageSpeed)),
		attribute.Int("part_count", actualPartCount),
		attribute.Int("concurrency", b.downloadConcurrency),
	)

	// Extend an object's effective TTL by performing CopyObject on itself to
	// refresh its LastModified timestamp.
	//
	// The CopySourceIfUnmodifiedSince precondition aborts the operation with an
	// HTTP status code 412 if the object's LastModified timestamp falls within
	// restoreRefreshMinInterval.
	//
	// This refresh is best-effort only: any failure (incl. objects exceeding
	// S3's 5GB CopyObject limit) must not cause the overall restore operation to fail.
	copySource := fmt.Sprintf("%s/%s", b.bucketName, fullKey)
	_, err = b.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:                      aws.String(b.bucketName),
		Key:                         aws.String(fullKey),
		CopySource:                  aws.String(copySource),
		MetadataDirective:           "REPLACE",
		CopySourceIfUnmodifiedSince: aws.Time(time.Now().Add(-restoreRefreshMinInterval)),
	})
	switch {
	case err == nil:
		slog.Debug("refreshed object expiration", "key", fullKey, "bucket", b.bucketName)
	case isPreconditionFailed(err):
		slog.Debug("skipping cache TTL refresh, blob modified recently",
			"key", fullKey, "bucket", b.bucketName)
	default:
		slog.Warn("failed to refresh object expiration, continuing (non-fatal)",
			"key", fullKey, "bucket", b.bucketName, "error", err)
	}

	return &TransferInfo{
		BytesTransferred: bytesWritten,
		TransferSpeed:    averageSpeed,
		RequestID:        "", // Download doesn't return a single request ID for parallel downloads
		Duration:         duration,
		PartCount:        actualPartCount,
		Concurrency:      b.downloadConcurrency,
	}, nil
}

// getFullKey combines the prefix with the key
func (b *S3Blob) getFullKey(key string) string {
	// Remove leading slash from key if present
	key = strings.TrimPrefix(key, "/")
	// Combine prefix and key
	return path.Join(b.prefix, key)
}
