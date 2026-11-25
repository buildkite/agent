package artifact

import (
	"fmt"
	"maps"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/logger"
)

func TestObjectAttributeConfiguration(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, destination, s3, gcs, azure string
		disabled                          bool
		explicit                          []string
		want                              map[string]string
		wantError                         bool
	}{
		{name: "disabled escape hatch", s3: "invalid", explicit: []string{"gcs-object-contexts"}, disabled: true},
		{name: "S3 ignores inactive defaults", destination: "s3://bucket", s3: `{"team":"payments","Team":"search","empty":""}`, gcs: "invalid", azure: "invalid", want: map[string]string{"team": "payments", "Team": "search", "empty": ""}},
		{name: "GCS contexts", destination: "gs://bucket", gcs: `{"pipeline":"payments","job":"abc-123"}`, want: map[string]string{"pipeline": "payments", "job": "abc-123"}},
		{name: "Azure tags", destination: "https://account.blob.core.windows.net/container/", azure: `{"team":"payments","empty":""}`, want: map[string]string{"team": "payments", "empty": ""}},
		{name: "clear inherited map", destination: "s3://bucket", s3: `{}`, want: map[string]string{}},
		{name: "hosted enable only"},
		{name: "Artifactory enable only", destination: "rt://repo"},
		{name: "explicit wrong provider", destination: "s3://bucket", explicit: []string{"gcs-object-contexts"}, wantError: true},
		{name: "explicit unsupported provider", explicit: []string{"s3-object-tags"}, wantError: true},
		{name: "hosted nonempty", azure: `{"team":"payments"}`, wantError: true},
		{name: "hosted invalid", s3: "invalid", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			conf := UploaderConfig{Destination: test.destination, ObjectAttributes: !test.disabled, S3ObjectTags: "{}", GCSObjectContexts: "{}", AzureBlobTags: "{}", ExplicitAttributeFlags: test.explicit}
			if test.s3 != "" {
				conf.S3ObjectTags = test.s3
			}
			if test.gcs != "" {
				conf.GCSObjectContexts = test.gcs
			}
			if test.azure != "" {
				conf.AzureBlobTags = test.azure
			}
			got, err := conf.resolveObjectAttributes()
			if (err != nil) != test.wantError {
				t.Fatalf("resolveObjectAttributes() error = %v, want error %v", err, test.wantError)
			}
			if !test.wantError && !maps.Equal(got, test.want) {
				t.Errorf("attributes = %v, want %v", got, test.want)
			}
		})
	}
}

func TestObjectAttributeLimits(t *testing.T) {
	t.Parallel()
	many := func(n int) string {
		pairs := make([]string, n)
		for i := range n {
			pairs[i] = fmt.Sprintf(`"k%d":"value"`, i)
		}
		return "{" + strings.Join(pairs, ",") + "}"
	}
	for _, test := range []struct {
		name, provider, raw string
		wantError           bool
	}{
		{"empty input", "s3", "", true},
		{"null map", "s3", "null", true},
		{"null member", "s3", `{"team":null}`, true},
		{"number member", "s3", `{"team":3}`, true},
		{"array", "s3", `[]`, true},
		{"scalar", "s3", `"foo"`, true},
		{"trailing JSON", "s3", `{} {}`, true},
		{"S3 maximum", "s3", many(10), false},
		{"S3 too many", "s3", many(11), true},
		{"S3 encoding characters", "s3", `{"pipeline":"payments/api + jobs=3","empty":""}`, false},
		{"S3 forbidden character", "s3", `{"team":"payments&api"}`, true},
		{"S3 reserved prefix", "s3", `{"aws:team":"payments"}`, true},
		{"S3 empty key", "s3", `{"":"payments"}`, true},
		{"S3 UTF16 key boundary", "s3", `{"` + strings.Repeat("𐐀", 64) + `":"value"}`, false},
		{"S3 UTF16 key overflow", "s3", `{"` + strings.Repeat("𐐀", 65) + `":"value"}`, true},
		{"S3 UTF16 value boundary", "s3", `{"team":"` + strings.Repeat("𐐀", 128) + `"}`, false},
		{"S3 UTF16 value overflow", "s3", `{"team":"` + strings.Repeat("𐐀", 129) + `"}`, true},
		{"GCS maximum", "gcs", many(50), false},
		{"GCS too many", "gcs", many(51), true},
		{"GCS UTF8 boundary", "gcs", `{"` + strings.Repeat("é", 128) + `":"` + strings.Repeat("é", 128) + `"}`, false},
		{"GCS UTF8 key overflow", "gcs", `{"` + strings.Repeat("é", 129) + `":"value"}`, true},
		{"GCS UTF8 value overflow", "gcs", `{"team":"` + strings.Repeat("é", 129) + `"}`, true},
		{"GCS reserved prefix", "gcs", `{"google:team":"payments"}`, true},
		{"GCS slash", "gcs", `{"pipeline":"payments/api"}`, true},
		{"GCS backslash", "gcs", `{"pipeline":"payments\\api"}`, true},
		{"GCS quote", "gcs", `{"pipeline":"payments\"api"}`, true},
		{"GCS empty value", "gcs", `{"team":""}`, true},
		{"GCS nonalphanumeric start", "gcs", `{"team":"-payments"}`, true},
		{"Azure maximum", "azure", many(10), false},
		{"Azure too many", "azure", many(11), true},
		{"Azure key boundary", "azure", `{"` + strings.Repeat("k", 128) + `":"value"}`, false},
		{"Azure key overflow", "azure", `{"` + strings.Repeat("k", 129) + `":"value"}`, true},
		{"Azure value boundary", "azure", `{"team":"` + strings.Repeat("v", 256) + `"}`, false},
		{"Azure value overflow", "azure", `{"team":"` + strings.Repeat("v", 257) + `"}`, true},
		{"Azure ASCII only", "azure", `{"team":"é"}`, true},
		{"Azure no newline", "azure", `{"team":"payments\napi"}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			conf := UploaderConfig{ObjectAttributes: true}
			switch test.provider {
			case "s3":
				conf.Destination, conf.S3ObjectTags = "s3://bucket", test.raw
			case "gcs":
				conf.Destination, conf.GCSObjectContexts = "gs://bucket", test.raw
			case "azure":
				conf.Destination, conf.AzureBlobTags = "https://account.blob.core.windows.net/container/", test.raw
			}
			_, err := conf.resolveObjectAttributes()
			if (err != nil) != test.wantError {
				t.Errorf("resolveObjectAttributes() error = %v, want error %v", err, test.wantError)
			}
		})
	}
}

func TestUploadAttributesValidateAfterCollectionBeforeRegistration(t *testing.T) {
	t.Parallel()
	for _, destination := range []string{"s3://bucket", "gs://bucket", "https://account.blob.core.windows.net/container/", "", "rt://repo"} {
		t.Run(destination, func(t *testing.T) {
			for _, matching := range []bool{false, true} {
				paths := filepath.Join(t.TempDir(), "missing-*.txt")
				if matching {
					paths = "fixtures/Mr Freeze.jpg"
				}
				// A nil API client also ensures rejection precedes registration.
				u := NewUploader(logger.Discard, nil, UploaderConfig{Paths: paths, Destination: destination, ObjectAttributes: true, S3ObjectTags: "private-value-not-json", GCSObjectContexts: "private-value-not-json", AzureBlobTags: "private-value-not-json"})
				err := u.Upload(t.Context())
				if (err != nil) != matching {
					t.Fatalf("Upload(matching=%v) error = %v", matching, err)
				}
				if err != nil && strings.Contains(err.Error(), "private-value") {
					t.Errorf("error exposed an attribute value: %v", err)
				}
			}
		})
	}
}
