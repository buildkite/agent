package agent

import (
	"fmt"
	"github.com/buildkite/agent/logger"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestArtifactDownloaderConnectsToEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case `/builds/my-build/artifacts/search`:
			fmt.Fprintf(rw, `[{
				"id": "4600ac5c-5a13-4e92-bb83-f86f218f7b32",
				"file_size": 3,
				"absolute_path": "llamas.txt",
				"path": "llamas.txt",
				"url": "http://%s/download"
			}]`, req.Host)
		case `/download`:
			fmt.Fprintln(rw, "OK")
		default:
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	ac := NewAPIClient(logger.Discard, APIClientConfig{
		Endpoint: server.URL,
		Token:    `llamasforever`,
	})

	d := NewArtifactDownloader(logger.Discard, ac, ArtifactDownloaderConfig{
		BuildID: "my-build",
	})

	err := d.Download()
	if err != nil {
		t.Fatal(err)
	}
}
