package artifact

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
)

func TestArtifactDownloaderConnectsToEndpoint(t *testing.T) {
	t.Cleanup(func() {
		os.Remove("llamas.txt") //nolint:errcheck // Best-effort cleanup.
	})

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.RequestURI() {
		case "/builds/my-build/artifacts/search?state=finished":
			//nolint:errcheck // Test should fail with incomplete response.
			fmt.Fprintf(rw, `[{
				"id": "4600ac5c-5a13-4e92-bb83-f86f218f7b32",
				"file_size": 3,
				"absolute_path": "llamas.txt",
				"path": "llamas.txt",
				"url": "http://%s/download"
			}]`, req.Host)
		case "/download":
			fmt.Fprintln(rw, "OK") //nolint:errcheck // YOLO?
		default:
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx := context.Background()

	ac := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamasforever",
	})

	d := NewDownloader(logger.Discard, ac, DownloaderConfig{
		BuildID: "my-build",
	})

	if err := d.Download(ctx); err != nil {
		t.Errorf("d.Download() = %v", err)
	}
}
