package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGCPMetaDataGetPaths(t *testing.T) {
	ctx := context.Background()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)

		switch path := r.URL.EscapedPath(); path {
		case "/computeMetadata/v1/value":
			_, _ = fmt.Fprintf(w, "I could live on only burritos for the rest of my life")
		case "/computeMetadata/v1/nested/paths/work":
			_, _ = fmt.Fprintf(w, "Velociraptors are terrifying")
		default:
			// NB: Do not use t.Fatal/Fatalf/FailNow from outside the test
			// runner goroutine. See https://pkg.go.dev/testing#T.FailNow
			http.Error(w, "not found: "+path, http.StatusNotFound)
		}
	}))
	t.Cleanup(ts.Close)

	url, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", ts.URL, err)
	}

	old := os.Getenv("GCE_METADATA_HOST")
	t.Cleanup(func() {
		os.Setenv("GCE_METADATA_HOST", old) //nolint:errcheck // Best-effort cleanup.
	})
	if err := os.Setenv("GCE_METADATA_HOST", url.Host); err != nil {
		t.Fatalf("os.Setenv(GCE_METADATA_HOST, %q) = %v", url.Host, err)
	}

	paths := map[string]string{
		"truth":     "value",
		"scary":     "nested/paths/work",
		"weird key": "value",
	}

	values, err := GCPMetaData{}.GetPaths(ctx, paths)
	if err != nil {
		t.Fatalf("GCPMetadata{}.GetPaths(%v) error = %v", paths, err)
	}

	if diff := cmp.Diff(values, map[string]string{
		"truth":     "I could live on only burritos for the rest of my life",
		"scary":     "Velociraptors are terrifying",
		"weird key": "I could live on only burritos for the rest of my life",
	}); diff != "" {
		t.Errorf("values diff (-got +want):\n%s", diff)
	}
}
