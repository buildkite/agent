package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	tmtypes "github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/buildkite/agent/v3/internal/agenthttp"
	"github.com/buildkite/agent/v3/internal/osutil"
	"github.com/buildkite/agent/v3/logger"
	"github.com/dustin/go-humanize"
)

const (
	s3MultipartDownloadPartSize    = 8 * 1024 * 1024 // 8 MiB, matches AWS CLI default
	s3MultipartDownloadConcurrency = 10              // matches AWS CLI default
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

	// Expected SHA-256 hex digest from the artifact API record. If non-empty,
	// the multipart path verifies the downloaded bytes against it before
	// atomic rename. Empty for legacy artifacts without a digest.
	ExpectedSHA256 string

	// Whether to allow multipart downloads to the custom s3 bucket
	AllowS3Multipart bool
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
		return fmt.Errorf("S3Downloader for %s: S3Client is nil", d.conf.S3Path)
	}
	if d.conf.AllowS3Multipart {
		return d.startMultipart(ctx)
	}
	return d.startSingle(ctx)
}

// startSingle downloads the entire object as a single stream by presigning a
// GET URL and handing it to the generic HTTP downloader. Reachable when
// --no-s3-multipart-download (BUILDKITE_NO_S3_MULTIPART_DOWNLOAD) is set —
// the kill-switch back to the legacy pre-multipart behaviour.
func (d S3Downloader) startSingle(ctx context.Context) error {
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

// startMultipart downloads the object via parallel ranged GETs using the AWS
// SDK transfer manager. This is the default and currently only reachable path.
//
// Unlike the single-stream path, parts arrive out of order via io.WriterAt, so
// we cannot hash inline. The temp file is hashed in a second pass after the
// download completes, both to log the digest and to verify against the
// uploader-provided SHA-256 when one is available.
func (d S3Downloader) startMultipart(ctx context.Context) error {
	targetPath := targetPath(ctx, d.conf.Path, d.conf.Destination)
	targetDirectory, targetFile := filepath.Split(targetPath)

	if err := os.MkdirAll(targetDirectory, 0o777); err != nil {
		return fmt.Errorf("creating directory for %s: %w", targetPath, err)
	}

	temp, err := os.CreateTemp(targetDirectory, targetFile)
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(temp.Name()) //nolint:errcheck // Best-effort cleanup.
	defer temp.Close()           //nolint:errcheck // Primary Close checked below.

	d.logger.Debugf("Multipart downloading s3://%s/%s to %s", d.BucketName(), d.BucketFileLocation(), targetPath)

	tmClient := transfermanager.New(d.conf.S3Client, func(o *transfermanager.Options) {
		o.PartSizeBytes = s3MultipartDownloadPartSize
		o.Concurrency = s3MultipartDownloadConcurrency
		// Use Range-based fan-out rather than partNumber-based. The SDK's
		// default (PART) only fans out for objects uploaded as multipart;
		// RANGE works for any object — matching the AWS CLI's behavior.
		o.GetObjectType = tmtypes.GetObjectRanges
		o.PartBodyMaxRetries = d.conf.Retries
	})

	out, err := tmClient.DownloadObject(ctx, &transfermanager.DownloadObjectInput{
		Bucket:   aws.String(d.BucketName()),
		Key:      aws.String(d.BucketFileLocation()),
		WriterAt: temp,
	})
	if err != nil {
		return fmt.Errorf("multipart S3 download of %s failed: %w", d.conf.S3Path, err)
	}
	bytes := aws.ToInt64(out.ContentLength)

	if err := temp.Chmod(0o666 &^ osutil.Umask); err != nil {
		return fmt.Errorf("setting file permissions: %w", err)
	}

	// Seek rewinds the file's read/write cursor to byte 0; without this, io.Copy
	// below would read from wherever the SDK left the cursor and hash garbage.
	if _, err := temp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seeking temp file for hash: %w", err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, temp); err != nil {
		return fmt.Errorf("hashing temp file: %w", err)
	}
	gotSHA256 := hex.EncodeToString(hash.Sum(nil))

	if d.conf.ExpectedSHA256 != "" && gotSHA256 != d.conf.ExpectedSHA256 {
		return fmt.Errorf("checksum of downloaded content %s != uploaded checksum %s", gotSHA256, d.conf.ExpectedSHA256)
	}

	if err := temp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}

	if err := os.Rename(temp.Name(), targetPath); err != nil {
		return fmt.Errorf("renaming temp file to target: %w", err)
	}

	d.logger.Infof("Successfully downloaded %q %s with SHA256 %s", d.conf.Path, humanize.IBytes(uint64(bytes)), gotSHA256)
	return nil
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
