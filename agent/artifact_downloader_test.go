package agent

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/assert"
)

func TestArtifactDownloaderConnectsToEndpoint(t *testing.T) {
	defer os.Remove("llamas.txt")

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

	ac := api.NewClient(logger.Discard, api.Config{
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

func TestGetDownloadDestination(t *testing.T) {
	workingDirectory, _ := filepath.Abs(".")
	assert.Equal(t, workingDirectory + string(os.PathSeparator) + "a", getDownloadDestination("a"))
	assert.Equal(t, workingDirectory + string(os.PathSeparator) + "a" + string(os.PathSeparator), getDownloadDestination("a" + string(os.PathSeparator)))

	// Test that we don't get a double // on unix, must use filepath.Abs
	// to handle the Windows case which normalises to C:\
	root, _ := filepath.Abs("/")
	assert.Equal(t, root, getDownloadDestination("/"))
}
