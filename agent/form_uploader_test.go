package agent

import (
	"bytes"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
)

func TestFormUploading(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case `/buildkiteartifacts.com`:
			err := req.ParseMultipartForm(5 * 1024 * 1024)
			if err != nil {
				t.Error(err)
				http.Error(rw, err.Error(), http.StatusInternalServerError)
				return
			}

			// Check the ${artifact:path} interpolation is working
			path := req.FormValue("path")
			if path != "llamas.txt" {
				t.Errorf("Bad path content %q", path)
				http.Error(rw, "Bad path content", http.StatusInternalServerError)
				return
			}

			file, _, err := req.FormFile("file")
			if err != nil {
				t.Error(err)
				http.Error(rw, err.Error(), http.StatusInternalServerError)
				return
			}
			defer file.Close()

			b := &bytes.Buffer{}
			_, _ = io.Copy(b, file)

			// Check the file is attached correctly
			if b.String() != "llamas" {
				t.Errorf("Bad file content %q", b.String())
				http.Error(rw, "Bad file content", http.StatusInternalServerError)
				return
			}

		default:
			t.Errorf("Unknown path %s %s", req.Method, req.URL.Path)
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	err = ioutil.WriteFile(filepath.Join(wd, "llamas.txt"), []byte("llamas"), 0700)
	if err != nil {
		t.Fatal(err)
	}

	defer os.Remove(filepath.Join(wd, "llamas.txt"))

	uploader := NewFormUploader(logger.Discard, FormUploaderConfig{})
	artifact := &api.Artifact{
		ID:           "xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxx",
		Path:         "llamas.txt",
		AbsolutePath: filepath.Join(wd, "llamas.txt"),
		GlobPath:     "llamas.txt",
		ContentType:  "text/plain",
		UploadInstructions: &api.ArtifactUploadInstructions{
			Data: map[string]string{
				"path": "${artifact:path}",
			},
			Action: struct {
				URL       string "json:\"url,omitempty\""
				Method    string "json:\"method\""
				Path      string "json:\"path\""
				FileInput string "json:\"file_input\""
			}{
				URL:       server.URL,
				Method:    "POST",
				Path:      "buildkiteartifacts.com",
				FileInput: "file",
			}},
	}

	if err := uploader.Upload(artifact); err != nil {
		t.Fatal(err)
	}
}
