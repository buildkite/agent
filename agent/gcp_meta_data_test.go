package agent

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGCPMetaDataGetPaths(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)

		switch path := r.URL.EscapedPath(); path {
		case "/computeMetadata/v1/value":
			fmt.Fprintf(w, "I could live on only burritos for the rest of my life")
		case "/computeMetadata/v1/nested/paths/work":
			fmt.Fprintf(w, "Velociraptors are terrifying")
		default:
			// NB: Do not use t.Fatal/Fatalf/FailNow from outside the test
			// runner goroutine. See https://pkg.go.dev/testing#T.FailNow
			http.Error(w, "not found: "+path, http.StatusNotFound)
		}
	}))
	defer ts.Close()

	url, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", ts.URL, err)
	}

	old := os.Getenv("GCE_METADATA_HOST")
	defer os.Setenv("GCE_METADATA_HOST", old)
	os.Setenv("GCE_METADATA_HOST", url.Host)

	paths := map[string]string{
		"truth":     "value",
		"scary":     "nested/paths/work",
		"weird key": "value",
	}

	values, err := GCPMetaData{}.GetPaths(paths)
	if err != nil {
		t.Fatalf("GCPMetadata{}.GetPaths(%v) error = %v", paths, err)
	}

	assert.Equal(t, values, map[string]string{
		"truth":     "I could live on only burritos for the rest of my life",
		"scary":     "Velociraptors are terrifying",
		"weird key": "I could live on only burritos for the rest of my life",
	})
}
