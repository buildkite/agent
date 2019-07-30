package agent

import (
	"fmt"
	// "github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"net/url"
	// "os"
	"testing"
)

func TestEC2MetaDataGetSuffixes(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)

		switch path := r.URL.EscapedPath(); path {
		default:
			t.Fatalf("Error %q", path)
		}
	}))
	defer ts.Close()

	url, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("Error %q", err)
	}
}
