package store

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithy "github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/buildkite/roko"
	"github.com/google/go-cmp/cmp"
)

func TestOptionsFromURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		want        *Options
		wantErr     bool
		errContains string
	}{
		{
			name: "simple s3 bucket",
			url:  "s3://my-bucket",
			want: &Options{
				Bucket:       "my-bucket",
				Region:       "us-east-1", // default
				Prefix:       "",
				S3Endpoint:   "",
				UsePathStyle: false,
			},
			wantErr: false,
		},
		{
			name: "s3 bucket with prefix",
			url:  "s3://my-bucket/cache/artifacts",
			want: &Options{
				Bucket:       "my-bucket",
				Region:       "us-east-1",
				Prefix:       "cache/artifacts",
				S3Endpoint:   "",
				UsePathStyle: false,
			},
			wantErr: false,
		},
		{
			name: "s3 bucket with trailing slash in prefix",
			url:  "s3://my-bucket/cache/artifacts/",
			want: &Options{
				Bucket:       "my-bucket",
				Region:       "us-east-1",
				Prefix:       "cache/artifacts",
				S3Endpoint:   "",
				UsePathStyle: false,
			},
			wantErr: false,
		},
		{
			name: "s3 bucket with region query param",
			url:  "s3://my-bucket?region=us-west-2",
			want: &Options{
				Bucket:       "my-bucket",
				Region:       "us-west-2",
				Prefix:       "",
				S3Endpoint:   "",
				UsePathStyle: false,
			},
			wantErr: false,
		},
		{
			name: "s3 bucket with prefix and region",
			url:  "s3://my-bucket/some/prefix?region=eu-central-1",
			want: &Options{
				Bucket:       "my-bucket",
				Region:       "eu-central-1",
				Prefix:       "some/prefix",
				S3Endpoint:   "",
				UsePathStyle: false,
			},
			wantErr: false,
		},
		{
			name: "s3 bucket with custom endpoint for local testing",
			url:  "s3://my-bucket?endpoint=http://localhost:9000",
			want: &Options{
				Bucket:       "my-bucket",
				Region:       "us-east-1",
				Prefix:       "",
				S3Endpoint:   "http://localhost:9000",
				UsePathStyle: false,
			},
			wantErr: false,
		},
		{
			name: "s3 bucket with path style access",
			url:  "s3://my-bucket?use_path_style=true",
			want: &Options{
				Bucket:       "my-bucket",
				Region:       "us-east-1",
				Prefix:       "",
				S3Endpoint:   "",
				UsePathStyle: true,
			},
			wantErr: false,
		},
		{
			name: "s3 bucket with all options",
			url:  "s3://my-bucket/prefix/path?region=ap-southeast-2&endpoint=http://localhost:9000&use_path_style=true",
			want: &Options{
				Bucket:       "my-bucket",
				Region:       "ap-southeast-2",
				Prefix:       "prefix/path",
				S3Endpoint:   "http://localhost:9000",
				UsePathStyle: true,
			},
			wantErr: false,
		},
		{
			name: "use_path_style=false is ignored",
			url:  "s3://my-bucket?use_path_style=false",
			want: &Options{
				Bucket:       "my-bucket",
				Region:       "us-east-1",
				Prefix:       "",
				S3Endpoint:   "",
				UsePathStyle: false,
			},
			wantErr: false,
		},
		{
			name: "s3 bucket with concurrency",
			url:  "s3://my-bucket?concurrency=10",
			want: &Options{
				Bucket:      "my-bucket",
				Region:      "us-east-1",
				Concurrency: 10,
			},
			wantErr: false,
		},
		{
			name: "s3 bucket with all options including concurrency",
			url:  "s3://my-bucket/prefix?region=eu-west-1&concurrency=20",
			want: &Options{
				Bucket:      "my-bucket",
				Region:      "eu-west-1",
				Prefix:      "prefix",
				Concurrency: 20,
			},
			wantErr: false,
		},
		{
			name:        "invalid concurrency value",
			url:         "s3://my-bucket?concurrency=abc",
			wantErr:     true,
			errContains: "invalid concurrency value",
		},
		{
			name: "concurrency of 0 means use default",
			url:  "s3://my-bucket?concurrency=0",
			want: &Options{
				Bucket:      "my-bucket",
				Region:      "us-east-1",
				Concurrency: 0,
			},
			wantErr: false,
		},
		{
			name:        "negative concurrency",
			url:         "s3://my-bucket?concurrency=-5",
			wantErr:     true,
			errContains: "concurrency must be between 0 and 100",
		},
		{
			name:        "concurrency exceeds maximum",
			url:         "s3://my-bucket?concurrency=101",
			wantErr:     true,
			errContains: "concurrency must be between 0 and 100",
		},
		{
			name: "part_size_mb valid value",
			url:  "s3://my-bucket?part_size_mb=10",
			want: &Options{
				Bucket:     "my-bucket",
				Region:     "us-east-1",
				PartSizeMB: 10,
			},
			wantErr: false,
		},
		{
			name: "part_size_mb of 0 means use default",
			url:  "s3://my-bucket?part_size_mb=0",
			want: &Options{
				Bucket:     "my-bucket",
				Region:     "us-east-1",
				PartSizeMB: 0,
			},
			wantErr: false,
		},
		{
			name: "part_size_mb maximum value (5GB)",
			url:  "s3://my-bucket?part_size_mb=5120",
			want: &Options{
				Bucket:     "my-bucket",
				Region:     "us-east-1",
				PartSizeMB: 5120,
			},
			wantErr: false,
		},
		{
			name:        "part_size_mb below minimum (5MB)",
			url:         "s3://my-bucket?part_size_mb=4",
			wantErr:     true,
			errContains: "part_size_mb must be 0 (default) or between 5 and 5120",
		},
		{
			name:        "part_size_mb exceeds maximum",
			url:         "s3://my-bucket?part_size_mb=5121",
			wantErr:     true,
			errContains: "part_size_mb must be 0 (default) or between 5 and 5120",
		},
		{
			name:        "part_size_mb negative value",
			url:         "s3://my-bucket?part_size_mb=-1",
			wantErr:     true,
			errContains: "part_size_mb must be 0 (default) or between 5 and 5120",
		},
		{
			name:        "part_size_mb invalid value",
			url:         "s3://my-bucket?part_size_mb=abc",
			wantErr:     true,
			errContains: "invalid part_size_mb value",
		},
		{
			name: "all transfer options combined",
			url:  "s3://my-bucket?concurrency=10&part_size_mb=100",
			want: &Options{
				Bucket:      "my-bucket",
				Region:      "us-east-1",
				Concurrency: 10,
				PartSizeMB:  100,
			},
			wantErr: false,
		},
		{
			name:        "invalid URL",
			url:         "://invalid",
			want:        nil,
			wantErr:     true,
			errContains: "failed to parse S3 URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := OptionsFromURL(tt.url)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("OptionsFromURL: %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("OptionsFromURL mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestIsPreconditionFailed(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "precondition failed API error (CopyObject refresh)",
			err:  &smithy.GenericAPIError{Code: "PreconditionFailed", Message: "At least one of the pre-conditions you specified did not hold"},
			want: true,
		},
		{
			name: "412 response error (download ETag change)",
			err:  responseErrorWithStatus(http.StatusPreconditionFailed),
			want: true,
		},
		{
			name: "different API error code",
			err:  &smithy.GenericAPIError{Code: "NoSuchKey", Message: "The specified key does not exist"},
			want: false,
		},
		{
			name: "non-412 response error",
			err:  responseErrorWithStatus(http.StatusInternalServerError),
			want: false,
		},
		{
			name: "plain error",
			err:  errors.New("boom"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPreconditionFailed(tt.err)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("isPreconditionFailed mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetFullKey(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		key    string
		want   string
	}{
		{
			name:   "no prefix",
			prefix: "",
			key:    "cache.tar.gz",
			want:   "cache.tar.gz",
		},
		{
			name:   "with prefix",
			prefix: "artifacts",
			key:    "cache.tar.gz",
			want:   "artifacts/cache.tar.gz",
		},
		{
			name:   "with nested prefix",
			prefix: "artifacts/builds",
			key:    "cache.tar.gz",
			want:   "artifacts/builds/cache.tar.gz",
		},
		{
			name:   "key with leading slash",
			prefix: "artifacts",
			key:    "/cache.tar.gz",
			want:   "artifacts/cache.tar.gz",
		},
		{
			name:   "key with path",
			prefix: "artifacts",
			key:    "project/cache.tar.gz",
			want:   "artifacts/project/cache.tar.gz",
		},
		{
			name:   "empty prefix and key",
			prefix: "",
			key:    "",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blob := &S3Blob{
				prefix: tt.prefix,
			}
			got := blob.getFullKey(tt.key)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestNewS3Blob exercises construction against a custom endpoint with
// path-style access and explicit transfer tuning — the URL options that
// self-hosted / S3-compatible backends rely on. Construction makes no network
// calls, so this verifies the transfermanager clients and settings are wired
// through from the URL without needing a live S3.
func TestNewS3Blob(t *testing.T) {
	blob, err := NewS3Blob(t.Context(),
		"s3://my-bucket/cache/prefix?region=us-west-2&endpoint=http://localhost:9000&use_path_style=true&concurrency=8&part_size_mb=16")
	if err != nil {
		t.Fatalf("NewS3Blob: %v", err)
	}

	if blob.client == nil {
		t.Error("client is nil")
	}
	if blob.uploader == nil {
		t.Error("uploader is nil")
	}
	if blob.downloader == nil {
		t.Error("downloader is nil")
	}
	if blob.bucketName != "my-bucket" {
		t.Errorf("bucketName = %q, want %q", blob.bucketName, "my-bucket")
	}
	if blob.prefix != "cache/prefix" {
		t.Errorf("prefix = %q, want %q", blob.prefix, "cache/prefix")
	}
	if blob.uploadConcurrency != 8 {
		t.Errorf("uploadConcurrency = %d, want 8", blob.uploadConcurrency)
	}
	if blob.downloadConcurrency != 8 {
		t.Errorf("downloadConcurrency = %d, want 8", blob.downloadConcurrency)
	}
	if want := int64(16 * 1024 * 1024); blob.downloadPartSize != want {
		t.Errorf("downloadPartSize = %d, want %d", blob.downloadPartSize, want)
	}
}

func TestResolveTransferSettings(t *testing.T) {
	const mb = int64(1024 * 1024)

	tests := []struct {
		name string
		opts *Options
		want transferSettings
	}{
		{
			name: "defaults differ between upload and download",
			opts: &Options{},
			want: transferSettings{
				uploadConcurrency:   defaultUploadConcurrency,
				uploadPartSize:      int64(defaultUploadPartSizeBytes),
				downloadConcurrency: defaultDownloadConcurrency,
				downloadPartSize:    int64(defaultDownloadPartSizeMB) * mb,
				maxIdleConnsPerHost: defaultDownloadConcurrency,
			},
		},
		{
			name: "concurrency override applies to both",
			opts: &Options{Concurrency: 50},
			want: transferSettings{
				uploadConcurrency:   50,
				uploadPartSize:      int64(defaultUploadPartSizeBytes),
				downloadConcurrency: 50,
				downloadPartSize:    int64(defaultDownloadPartSizeMB) * mb,
				maxIdleConnsPerHost: 50,
			},
		},
		{
			name: "part size override applies to both",
			opts: &Options{PartSizeMB: 64},
			want: transferSettings{
				uploadConcurrency:   defaultUploadConcurrency,
				uploadPartSize:      64 * mb,
				downloadConcurrency: defaultDownloadConcurrency,
				downloadPartSize:    64 * mb,
				maxIdleConnsPerHost: defaultDownloadConcurrency,
			},
		},
		{
			name: "both overrides applied",
			opts: &Options{Concurrency: 8, PartSizeMB: 16},
			want: transferSettings{
				uploadConcurrency:   8,
				uploadPartSize:      16 * mb,
				downloadConcurrency: 8,
				downloadPartSize:    16 * mb,
				maxIdleConnsPerHost: 8,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTransferSettings(tt.opts)
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(transferSettings{})); diff != "" {
				t.Errorf("resolveTransferSettings() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// responseErrorWithStatus builds a synthetic *awshttp.ResponseError carrying the
// given HTTP status code, matching what the SDK surfaces on a failed request.
func responseErrorWithStatus(status int) error {
	return &awshttp.ResponseError{
		ResponseError: &smithyhttp.ResponseError{
			Response: &smithyhttp.Response{Response: &http.Response{StatusCode: status}},
			Err:      fmt.Errorf("status %d", status),
		},
	}
}

// fakeDownloader is a test double for objectDownloader. It returns the result
// for each successive call from results (the last entry repeats if exhausted),
// writing payload to the destination on success so callers can assert bytes.
type fakeDownloader struct {
	calls   int
	results []fakeDownloadResult
}

type fakeDownloadResult struct {
	err     error
	payload []byte
}

func (f *fakeDownloader) DownloadObject(_ context.Context, in *transfermanager.DownloadObjectInput, _ ...func(*transfermanager.Options)) (*transfermanager.DownloadObjectOutput, error) {
	res := f.results[min(f.calls, len(f.results)-1)]
	f.calls++
	if res.err != nil {
		return nil, res.err
	}
	n, err := in.WriterAt.WriteAt(res.payload, 0)
	if err != nil {
		return nil, err
	}
	return &transfermanager.DownloadObjectOutput{ContentLength: aws.Int64(int64(n))}, nil
}

// testRetrier builds a retrier that runs instantly (no real sleeps) so the
// retry-loop tests are deterministic and fast.
func testRetrier() *roko.Retrier {
	return roko.NewRetrier(
		roko.WithMaxAttempts(3),
		roko.WithStrategy(roko.Constant(0)),
		roko.WithSleepFunc(func(time.Duration) {}),
	)
}

func TestDownloadWithRetry(t *testing.T) {
	payload := []byte("cache-contents")

	t.Run("retries 412 then succeeds", func(t *testing.T) {
		destPath := filepath.Join(t.TempDir(), "dest")
		fake := &fakeDownloader{results: []fakeDownloadResult{
			{err: responseErrorWithStatus(http.StatusPreconditionFailed)},
			{payload: payload},
		}}

		n, err := downloadWithRetry(t.Context(), testRetrier(), fake, destPath, &transfermanager.DownloadObjectInput{})
		if err != nil {
			t.Fatalf("downloadWithRetry: unexpected error: %v", err)
		}
		if fake.calls != 2 {
			t.Errorf("downloader called %d times, want 2", fake.calls)
		}
		if n != int64(len(payload)) {
			t.Errorf("bytes written = %d, want %d", n, len(payload))
		}
		got, err := os.ReadFile(destPath)
		if err != nil {
			t.Fatalf("read dest: %v", err)
		}
		if string(got) != string(payload) {
			t.Errorf("dest content = %q, want %q", got, payload)
		}
	})

	t.Run("persistent 412 exhausts attempts", func(t *testing.T) {
		destPath := filepath.Join(t.TempDir(), "dest")
		fake := &fakeDownloader{results: []fakeDownloadResult{
			{err: responseErrorWithStatus(http.StatusPreconditionFailed)},
		}}

		_, err := downloadWithRetry(t.Context(), testRetrier(), fake, destPath, &transfermanager.DownloadObjectInput{})
		if err == nil {
			t.Fatal("downloadWithRetry: expected error, got nil")
		}
		if !isPreconditionFailed(err) {
			t.Errorf("error %v is not a 412 PreconditionFailed", err)
		}
		if fake.calls != 3 {
			t.Errorf("downloader called %d times, want 3", fake.calls)
		}
	})

	t.Run("non-412 error fails without retry", func(t *testing.T) {
		destPath := filepath.Join(t.TempDir(), "dest")
		fake := &fakeDownloader{results: []fakeDownloadResult{
			{err: responseErrorWithStatus(http.StatusInternalServerError)},
		}}

		_, err := downloadWithRetry(t.Context(), testRetrier(), fake, destPath, &transfermanager.DownloadObjectInput{})
		if err == nil {
			t.Fatal("downloadWithRetry: expected error, got nil")
		}
		if fake.calls != 1 {
			t.Errorf("downloader called %d times, want 1", fake.calls)
		}
	})
}

// TestIsNotFound covers the classification that Download maps to
// store.ErrBlobNotFound: a missing S3 object surfaces as a typed
// *types.NoSuchKey (even when wrapped), while other errors must not match.
func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "typed NoSuchKey", err: &types.NoSuchKey{}, want: true},
		{name: "wrapped NoSuchKey", err: fmt.Errorf("download failed: %w", &types.NoSuchKey{}), want: true},
		{name: "precondition failed (412)", err: responseErrorWithStatus(http.StatusPreconditionFailed), want: false},
		{name: "internal error (500)", err: responseErrorWithStatus(http.StatusInternalServerError), want: false},
		{name: "plain error", err: errors.New("boom"), want: false},
		{name: "nil", err: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNotFound(tt.err); got != tt.want {
				t.Errorf("isNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
