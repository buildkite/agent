package artifact

import (
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestParseS3Destination(t *testing.T) {
	for _, tc := range []struct {
		dest, bucket, path string
	}{
		{
			dest:   "s3://my-bucket-name/foo/bar",
			bucket: "my-bucket-name",
			path:   "foo/bar",
		},
		{
			dest:   "s3://starts-with-an-s/and-this-is-its/folder",
			bucket: "starts-with-an-s",
			path:   "and-this-is-its/folder",
		},
		{
			dest:   "s3://custom-s3-domain/folder/ends-with-a-slash/",
			bucket: "custom-s3-domain",
			path:   "folder/ends-with-a-slash",
		},
	} {
		bucket, path := ParseS3Destination(tc.dest)
		if bucket != tc.bucket || path != tc.path {
			t.Errorf("ParseS3Destination(%q) = (%q, %q), want (%q, %q)", tc.dest, bucket, path, tc.bucket, tc.path)
		}
	}
}

func TestResolveServerSideEncryptionConfig(t *testing.T) {
	// Test manipulates env vars, so not parallel.
	tests := []struct {
		config string
		want   bool
	}{
		{config: "True", want: true},
		{config: "falsE", want: false},
		{config: "lol", want: false},
	}

	for _, test := range tests {
		uploader := &S3Uploader{}
		if err := os.Setenv("BUILDKITE_S3_SSE_ENABLED", test.config); err != nil {
			t.Fatalf("os.Setenv(BUILDKITE_S3_SSE_ENABLED, %q) = %v", test.config, err)
		}
		t.Cleanup(func() {
			os.Unsetenv("BUILDKITE_S3_SSE_ENABLED") //nolint:errcheck // Best-effort cleanup.
		})

		if got := uploader.serverSideEncryptionEnabled(); got != test.want {
			t.Errorf("BUILDKITE_S3_SSE_ENABLED=%q uploader.serverSideEncryptionEnabled() = %t, want %t", test.config, got, test.want)
		}
	}
}

func TestResolvePermission(t *testing.T) {
	// Test manipulates env vars, so not parallel.
	tests := []struct {
		permission string
		want       types.ObjectCannedACL
	}{
		{
			permission: "",
			want:       types.ObjectCannedACLPublicRead,
		},
		{
			permission: "private",
			want:       types.ObjectCannedACLPrivate,
		},
		{
			permission: "public-read",
			want:       types.ObjectCannedACLPublicRead,
		},
		{
			permission: "public-read-write",
			want:       types.ObjectCannedACLPublicReadWrite,
		},
		{
			permission: "authenticated-read",
			want:       types.ObjectCannedACLAuthenticatedRead,
		},
		{
			permission: "bucket-owner-read",
			want:       types.ObjectCannedACLBucketOwnerRead,
		},
		{
			permission: "bucket-owner-full-control",
			want:       types.ObjectCannedACLBucketOwnerFullControl,
		},
	}

	for _, test := range tests {
		t.Run(test.permission, func(t *testing.T) {
			// Test manipulates env vars, so not parallel.
			uploader := &S3Uploader{}
			if err := os.Setenv("BUILDKITE_S3_ACL", test.permission); err != nil {
				t.Fatalf("os.Setenv(BUILDKITE_S3_ACL, %q) = %v", test.permission, err)
			}
			t.Cleanup(func() {
				os.Unsetenv("BUILDKITE_S3_ACL") //nolint:errcheck // Best-effort cleanup.
			})
			got, err := uploader.resolvePermission()
			if err != nil {
				t.Fatalf("BUILDKITE_S3_ACL=%q uploader.resolvePermission() error = %v", test.permission, err)
			}
			if got != test.want {
				t.Errorf("BUILDKITE_S3_ACL=%q uploader.resolvePermission() = %v, want %v", test.permission, got, test.want)
			}
		})
	}
}

func TestResolvePermission_InvalidPermission(t *testing.T) {
	// Test manipulates env vars, so not parallel.
	permission := "foo"
	uploader := &S3Uploader{}
	if err := os.Setenv("BUILDKITE_S3_ACL", permission); err != nil {
		t.Fatalf("os.Setenv(BUILDKITE_S3_ACL, %q) = %v", permission, err)
	}
	t.Cleanup(func() {
		os.Unsetenv("BUILDKITE_S3_ACL") //nolint:errcheck // Best-effort cleanup.
	})
	wantErr := invalidACLError(permission)
	_, err := uploader.resolvePermission()
	if err != wantErr {
		t.Errorf("BUILDKITE_S3_ACL=%q uploader.resolvePermission() error = %v, want %v", permission, err, wantErr)
	}
}
