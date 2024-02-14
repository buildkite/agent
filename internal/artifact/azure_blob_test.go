package artifact

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseAzureBlobLocation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		desc  string
		input string
		want  *AzureBlobLocation
	}{
		{
			desc:  "example path",
			input: "https://asdf.blob.core.windows.net/my-container/blob.txt",
			want: &AzureBlobLocation{
				StorageAccountName: "asdf",
				ContainerName:      "my-container",
				BlobPath:           "blob.txt",
			},
		},
		{
			desc:  "more complex path",
			input: "https://asdf.blob.core.windows.net/my-container/some-directory/blob.txt?the-ignored-bit",
			want: &AzureBlobLocation{
				StorageAccountName: "asdf",
				ContainerName:      "my-container",
				BlobPath:           "some-directory/blob.txt",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			got, err := ParseAzureBlobLocation(test.input)
			if err != nil {
				t.Errorf("ParseAzureBlobLocation(%q) error = %v", test.input, err)
			}

			if diff := cmp.Diff(got, test.want); diff != "" {
				t.Errorf("parsed AzureBlobLocation diff (-got +want):\n%s", diff)
			}

			if !IsAzureBlobPath(test.input) {
				t.Errorf("IsAzureBlobPath(%q) = false, want true", test.input)
			}
		})
	}
}

func TestParseAzureBlobLocationErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		desc  string
		input string
	}{
		{
			desc:  "not https",
			input: "s3://azzyazzyazzyoioioi.blob.core.windows.net/container/file.txt",
		},
		{
			desc:  "not .blob.core.windows.net",
			input: "https://azzyazzyazzyoioioi.blorb.clorb.windorbs.net/container/file.txt",
		},
		{
			desc:  "no container",
			input: "https://azzyazzyazzyoioioi.blob.core.windows.net/file.txt",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			if _, err := ParseAzureBlobLocation(test.input); err == nil {
				t.Errorf("ParseAzureBlobLocation(%q) error = %v, want non-nil error", test.input, err)
			}

			if IsAzureBlobPath(test.input) {
				t.Errorf("IsAzureBlobPath(%q) = true, want false", test.input)
			}
		})
	}
}

func TestAzureBlobLocationURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		desc string
		base *AzureBlobLocation
		blob string
		want string
	}{
		{
			desc: "example path",
			base: &AzureBlobLocation{
				StorageAccountName: "asdf",
				ContainerName:      "my-container",
				BlobPath:           "",
			},
			blob: "blob.txt",
			want: "https://asdf.blob.core.windows.net/my-container/blob.txt",
		},
		{
			desc: "more complex path",
			base: &AzureBlobLocation{
				StorageAccountName: "asdf",
				ContainerName:      "my-container",
				BlobPath:           "some-directory",
			},
			blob: "blob.txt",
			want: "https://asdf.blob.core.windows.net/my-container/some-directory/blob.txt",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			got := test.base.URL(test.blob)
			if diff := cmp.Diff(got, test.want); diff != "" {
				t.Errorf("AzureBlobLocation.URL(%q) diff (-got +want):\n%s", test.blob, diff)
			}
		})
	}
}
