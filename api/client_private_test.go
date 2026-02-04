package api

import (
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRequestHeadersFromEnv(t *testing.T) {
	environ := []string{
		"HOME=/where/the/heart/is",
		"BUILDKITE_REQUEST_HEADER_BUILDKITE_FOO=bar",
		"BUILDKITE_REQUEST_HEADER_BUILDKITE_ANOTHER_THING=something else",
		"BUILDKITE_REQUEST_HEADER_CONTENT_TYPE=reject/this",
	}
	want := http.Header{
		"Buildkite-Foo":           []string{"bar"},
		"Buildkite-Another-Thing": []string{"something else"},
	}
	got := requestHeadersFromEnv(environ)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("requestHeadersFromEnv -want +got:\n%s", diff)
	}
}
