package store

import (
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
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

func TestShouldRefreshExpiration(t *testing.T) {
	last := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	expires := last.Add(10 * 24 * time.Hour) // 10-day lifecycle window
	fraction := 0.20                         // refresh point is the last 2 days

	tests := []struct {
		name         string
		expiresAt    time.Time
		lastModified time.Time
		now          time.Time
		want         bool
	}{
		{
			name:         "zero expiresAt refreshes",
			expiresAt:    time.Time{},
			lastModified: last,
			now:          last.Add(5 * 24 * time.Hour),
			want:         true,
		},
		{
			name:         "zero lastModified refreshes",
			expiresAt:    expires,
			lastModified: time.Time{},
			now:          last.Add(5 * 24 * time.Hour),
			want:         true,
		},
		{
			name:         "non-positive window refreshes",
			expiresAt:    last,
			lastModified: last.Add(time.Hour),
			now:          last,
			want:         true,
		},
		{
			name:         "half the window remaining skips",
			expiresAt:    expires,
			lastModified: last,
			now:          last.Add(5 * 24 * time.Hour),
			want:         false,
		},
		{
			name:         "exactly 20% remaining refreshes",
			expiresAt:    expires,
			lastModified: last,
			now:          last.Add(8 * 24 * time.Hour),
			want:         true,
		},
		{
			name:         "10% remaining refreshes",
			expiresAt:    expires,
			lastModified: last,
			now:          last.Add(9 * 24 * time.Hour),
			want:         true,
		},
		{
			name:         "already expired refreshes",
			expiresAt:    expires,
			lastModified: last,
			now:          last.Add(11 * 24 * time.Hour),
			want:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRefreshExpiration(tt.expiresAt, tt.lastModified, tt.now, fraction)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("shouldRefreshExpiration mismatch (-want +got):\n%s", diff)
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
				uploadConcurrency:   manager.DefaultUploadConcurrency,
				uploadPartSize:      manager.DefaultUploadPartSize,
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
				uploadPartSize:      manager.DefaultUploadPartSize,
				downloadConcurrency: 50,
				downloadPartSize:    int64(defaultDownloadPartSizeMB) * mb,
				maxIdleConnsPerHost: 50,
			},
		},
		{
			name: "part size override applies to both",
			opts: &Options{PartSizeMB: 64},
			want: transferSettings{
				uploadConcurrency:   manager.DefaultUploadConcurrency,
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
