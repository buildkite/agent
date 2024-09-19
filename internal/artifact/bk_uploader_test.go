package artifact

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

	for _, wd := range []string{temp, cwd} {
		t.Run(wd, func(t *testing.T) {
			abspath := filepath.Join(wd, "llamas.txt")
			err = os.WriteFile(abspath, []byte("llamas"), 0700)
			defer os.Remove(abspath)

			uploader := NewBKUploader(logger.Discard, BKUploaderConfig{})
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
					Action: api.ArtifactUploadAction{
						URL:       server.URL,
						Method:    "POST",
						Path:      "buildkiteartifacts.com",
						FileInput: "file",
					},
				},
			}

			work, err := uploader.CreateWork(artifact)
			if err != nil {
				t.Fatalf("uploader.CreateWork(artifact) error = %v", err)
			}

			for _, wu := range work {
				if err := wu.DoWork(ctx); err != nil {
					t.Errorf("bkUploaderWork.DoWork(artifact) = %v", err)
				}
			}
		})
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

	uploader := NewBKUploader(logger.Discard, BKUploaderConfig{})
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
			Action: api.ArtifactUploadAction{
				URL:       server.URL,
				Method:    "POST",
				Path:      "buildkiteartifacts.com",
				FileInput: "file",
			}},
	}

	work, err := uploader.CreateWork(artifact)
	if err != nil {
		t.Fatalf("uploader.CreateWork(artifact) error = %v", err)
	}

	for _, wu := range work {
		if err := wu.DoWork(ctx); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("bkUploaderWork.DoWork(artifact) = %v, want %v", err, fs.ErrNotExist)
		}
	}
}

func TestFormUploadTooBig(t *testing.T) {
	uploader := NewBKUploader(logger.Discard, BKUploaderConfig{})
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

	wantErr := errArtifactTooLarge{Size: size}
	if _, err := uploader.CreateWork(artifact); !errors.Is(err, wantErr) {
		t.Fatalf("uploader.CreateWork(artifact) error = %v, want %v", err, wantErr)
	}
}
