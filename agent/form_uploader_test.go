package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
)

func TestFormUploading(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/buildkiteartifacts.com":
			if req.ContentLength <= 0 {
				http.Error(rw, "zero or unknown Content-Length", http.StatusBadRequest)
				return
			}

			if err := req.ParseMultipartForm(5 * 1024 * 1024); err != nil {
				http.Error(rw, fmt.Sprintf("req.ParseMultipartForm() = %v", err), http.StatusBadRequest)
				return
			}

			// Check the ${artifact:path} interpolation is working
			path := req.FormValue("path")
			if got, want := path, "llamas.txt"; got != want {
				http.Error(rw, fmt.Sprintf("path = %q, want %q", got, want), http.StatusBadRequest)
				return
			}

			file, fh, err := req.FormFile("file")
			if err != nil {
				http.Error(rw, fmt.Sprintf(`req.FormFile("file") error = %v`, err), http.StatusBadRequest)
				return
			}
			defer file.Close()

			b := &bytes.Buffer{}
			if _, err := io.Copy(b, file); err != nil {
				http.Error(rw, fmt.Sprintf("io.Copy() error = %v", err), http.StatusInternalServerError)
				return
			}

			// Check the file is attached correctly
			if got, want := b.String(), "llamas"; got != want {
				http.Error(rw, fmt.Sprintf("uploaded file content = %q, want %q", got, want), http.StatusBadRequest)
				return
			}

			if got, want := fh.Filename, "llamas.txt"; got != want {
				http.Error(rw, fmt.Sprintf("uploaded file name = %q, want %q", got, want), http.StatusInternalServerError)
				return
			}

		default:
			http.Error(rw, fmt.Sprintf("not found; method = %q, path = %q", req.Method, req.URL.Path), http.StatusNotFound)
		}
	}))
	defer server.Close()

	temp, err := os.MkdirTemp("", "agent")
	if err != nil {
		t.Fatalf(`os.MkdirTemp("", "agent") error = %v`, err)
	}
	defer os.Remove(temp)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v", err)
	}

	runtest := func(wd string) {
		abspath := filepath.Join(wd, "llamas.txt")
		err = os.WriteFile(abspath, []byte("llamas"), 0700)
		defer os.Remove(abspath)

		uploader := NewFormUploader(logger.Discard, FormUploaderConfig{})
		artifact := &api.Artifact{
			ID:           "xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxx",
			Path:         "llamas.txt",
			AbsolutePath: abspath,
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

		if err := uploader.Upload(ctx, artifact); err != nil {
			t.Errorf("uploader.Upload(artifact) = %v", err)
		}
	}

	for _, wd := range []string{temp, cwd} {
		runtest(wd)
	}
}

func TestFormUploadFileMissing(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		http.Error(rw, "Not found", http.StatusNotFound)
	}))
	defer server.Close()

	temp, err := os.MkdirTemp("", "agent")
	if err != nil {
		t.Fatalf(`os.MkdirTemp("", "agent") error = %v`, err)
	}
	defer os.Remove(temp)

	abspath := filepath.Join(temp, "llamas.txt")

	uploader := NewFormUploader(logger.Discard, FormUploaderConfig{})
	artifact := &api.Artifact{
		ID:           "xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxx",
		Path:         "llamas.txt",
		AbsolutePath: abspath,
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

	if err := uploader.Upload(ctx, artifact); !os.IsNotExist(err) {
		t.Errorf("uploader.Upload(artifact) = %v, want os.ErrNotExist", err)
	}
}

func TestFormUploadTooBig(t *testing.T) {
	ctx := context.Background()
	uploader := NewFormUploader(logger.Discard, FormUploaderConfig{})
	const size = int64(6442450944) // 6Gb
	artifact := &api.Artifact{
		ID:                 "xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxx",
		Path:               "llamas.txt",
		AbsolutePath:       "/llamas.txt",
		GlobPath:           "llamas.txt",
		ContentType:        "text/plain",
		FileSize:           size,
		UploadInstructions: &api.ArtifactUploadInstructions{},
	}

	if err := uploader.Upload(ctx, artifact); !errors.Is(err, errArtifactTooLarge{Size: size}) {
		t.Errorf("uploader.Upload(artifact) = %v, want errArtifactTooLarge", err)
	}
}
