package artifact

import (
	"context"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/google/go-cmp/cmp"
)

var fakeProviderCreds = aws.Credentials{
	AccessKeyID:     "fakeProvider.AccessKeyID",
	SecretAccessKey: "fakeProvider.SecretAccessKey",
	SessionToken:    "fakeProvider.SessionToken",
	Source:          "fakeProvider",
	AccountID:       "fakeProvider.AccountID",
}

type fakeProvider struct{}

func (f fakeProvider) Retrieve(ctx context.Context) (aws.Credentials, error) {
	return fakeProviderCreds, nil
}

func TestBuildkiteEnvProvider(t *testing.T) {
	// Manipulates env vars, so no parallel.
	tests := []struct {
		name string
		env  map[string]string
		want aws.Credentials
	}{
		{
			name: "no env",
			env:  map[string]string{},
			want: fakeProviderCreds,
		},
		{
			name: "bk vars 1",
			env: map[string]string{
				"BUILDKITE_S3_ACCESS_KEY_ID":     "buildkite s3 access key id",
				"BUILDKITE_S3_SECRET_ACCESS_KEY": "buildkite s3 secret access key",
				"BUILDKITE_S3_SESSION_TOKEN":     "buildkite s3 session token",
			},
			want: aws.Credentials{
				CanExpire:       false,
				AccessKeyID:     "buildkite s3 access key id",
				SecretAccessKey: "buildkite s3 secret access key",
				SessionToken:    "buildkite s3 session token",
				Source:          "buildkiteEnvProvider",
			},
		},
		{
			name: "bk vars 2",
			env: map[string]string{
				"BUILDKITE_S3_ACCESS_KEY":    "buildkite s3 access key",
				"BUILDKITE_S3_SECRET_KEY":    "buildkite s3 secret key",
				"BUILDKITE_S3_SESSION_TOKEN": "buildkite s3 session token",
			},
			want: aws.Credentials{
				CanExpire:       false,
				AccessKeyID:     "buildkite s3 access key",
				SecretAccessKey: "buildkite s3 secret key",
				SessionToken:    "buildkite s3 session token",
				Source:          "buildkiteEnvProvider",
			},
		},
		{
			name: "session token missing is OK",
			env: map[string]string{
				"BUILDKITE_S3_ACCESS_KEY_ID":     "buildkite s3 access key id",
				"BUILDKITE_S3_SECRET_ACCESS_KEY": "buildkite s3 secret access key",
			},
			want: aws.Credentials{
				CanExpire:       false,
				AccessKeyID:     "buildkite s3 access key id",
				SecretAccessKey: "buildkite s3 secret access key",
				Source:          "buildkiteEnvProvider",
			},
		},
		{
			name: "access key ID missing fallback",
			env: map[string]string{
				"BUILDKITE_S3_SECRET_ACCESS_KEY": "buildkite s3 secret access key",
				"BUILDKITE_S3_SESSION_TOKEN":     "buildkite s3 session token",
			},
			want: fakeProviderCreds,
		},
		{
			name: "secret access key missing fallback",
			env: map[string]string{
				"BUILDKITE_S3_ACCESS_KEY_ID": "buildkite s3 access key id",
				"BUILDKITE_S3_SESSION_TOKEN": "buildkite s3 session token",
			},
			want: fakeProviderCreds,
		},
	}

	ctx := t.Context()

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for n, v := range test.env {
				if err := os.Setenv(n, v); err != nil {
					t.Fatalf("os.Setenv(%q, %q) = %v", n, v, err)
				}
				t.Cleanup(func() {
					os.Unsetenv(n) //nolint:errcheck // Best-effort cleanup
				})
			}

			got, err := buildkiteEnvProvider{next: fakeProvider{}}.Retrieve(ctx)
			if err != nil {
				t.Errorf("env=%v buildkiteEnvProvider{}.Retrieve(ctx) error = %v", test.env, err)
			}
			if diff := cmp.Diff(got, test.want); diff != "" {
				t.Errorf("env=%v buildkiteEnvProvider{}.Retrieve(ctx) diff (-got +want):\n%s", test.env, diff)
			}
		})
	}
}
