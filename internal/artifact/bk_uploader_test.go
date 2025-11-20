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
	"strconv"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestFormUploading(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.Method != "POST" && req.URL.Path != "/buildkiteartifacts.com" {
			http.Error(rw, fmt.Sprintf("not found; (method, path) = (%q, %q), want PUT /llamas3.txt", req.Method, req.URL.Path), http.StatusNotFound)
			return
		}

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
		defer file.Close() //nolint:errcheck // File open for read only.

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
	}))
	defer server.Close()

	temp, err := os.MkdirTemp("", "agent")
	if err != nil {
		t.Fatalf(`os.MkdirTemp("", "agent") error = %v`, err)
	}
	t.Cleanup(func() {
		os.RemoveAll(temp) //nolint:errcheck // Best-effort cleanup.
	})

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v", err)
	}

	for _, wd := range []string{temp, cwd} {
		t.Run(wd, func(t *testing.T) {
			abspath := filepath.Join(wd, "llamas.txt")
			if err := os.WriteFile(abspath, []byte("llamas"), 0o700); err != nil {
				t.Fatalf("os.WriteFile(%q, llamas, 0o700) = %v", abspath, err)
			}
			t.Cleanup(func() {
				os.Remove(abspath) //nolint:errcheck // Best-effort cleanup.
			})

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
				if _, err := wu.DoWork(ctx); err != nil {
					t.Errorf("bkUploaderWork.DoWork(ctx) = %v", err)
				}
			}
		})
	}
}

func TestMultipartUploading(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if req.Method != "PUT" || req.URL.Path != "/llamas3.txt" {
			http.Error(rw, fmt.Sprintf("not found; (method, path) = (%q, %q), want PUT /llamas3.txt", req.Method, req.URL.Path), http.StatusNotFound)
			return
		}
		partNum, err := strconv.Atoi(req.URL.Query().Get("partNumber"))
		if err != nil {
			http.Error(rw, fmt.Sprintf("strconv.Atoi(req.URL.Query().Get(partNumber)) error = %v", err), http.StatusBadRequest)
			return
		}

		if partNum < 1 || partNum > 3 {
			http.Error(rw, fmt.Sprintf("partNumber %d out of range [1, 3]", partNum), http.StatusBadRequest)
			return
		}

		b, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(rw, fmt.Sprintf("io.ReadAll(req.Body) error = %v", err), http.StatusInternalServerError)
			return
		}

		if got, want := string(b), "llamas"; got != want {
			http.Error(rw, fmt.Sprintf("req.Body = %q, want %q", got, want), http.StatusExpectationFailed)
		}

		rw.Header().Set("ETag", fmt.Sprintf(`"part number %d"`, partNum))
		rw.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	temp, err := os.MkdirTemp("", "agent")
	if err != nil {
		t.Fatalf(`os.MkdirTemp("", "agent") error = %v`, err)
	}
	t.Cleanup(func() {
		os.RemoveAll(temp) //nolint:errcheck // Best-effort cleanup.
	})

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v", err)
	}

	for _, wd := range []string{temp, cwd} {
		t.Run(wd, func(t *testing.T) {
			abspath := filepath.Join(wd, "llamas3.txt")
			if err := os.WriteFile(abspath, []byte("llamasllamasllamas"), 0o700); err != nil {
				t.Fatalf("os.WriteFile(%q, llamasllamasllamas, 0o700) = %v", abspath, err)
			}
			t.Cleanup(func() {
				os.Remove(abspath) //nolint:errcheck // Best-effort cleanup.
			})

			uploader := NewBKUploader(logger.Discard, BKUploaderConfig{})
			actions := []api.ArtifactUploadAction{
				{URL: server.URL + "/llamas3.txt?partNumber=1", Method: "PUT", PartNumber: 1},
				{URL: server.URL + "/llamas3.txt?partNumber=2", Method: "PUT", PartNumber: 2},
				{URL: server.URL + "/llamas3.txt?partNumber=3", Method: "PUT", PartNumber: 3},
			}
			artifact := &api.Artifact{
				ID:           "xxxxx-xxxx-xxxx-xxxx-xxxxxxxxxx",
				Path:         "llamas3.txt",
				AbsolutePath: abspath,
				GlobPath:     "llamas3.txt",
				FileSize:     18,
				ContentType:  "text/plain",
				UploadInstructions: &api.ArtifactUploadInstructions{
					Actions: actions,
				},
			}

			work, err := uploader.CreateWork(artifact)
			if err != nil {
				t.Fatalf("uploader.CreateWork(artifact) error = %v", err)
			}

			want := []workUnit{
				&bkMultipartUpload{BKUploader: uploader, artifact: artifact, partCount: 3, action: &actions[0], offset: 0, size: 6},
				&bkMultipartUpload{BKUploader: uploader, artifact: artifact, partCount: 3, action: &actions[1], offset: 6, size: 6},
				&bkMultipartUpload{BKUploader: uploader, artifact: artifact, partCount: 3, action: &actions[2], offset: 12, size: 6},
			}

			if diff := cmp.Diff(work, want,
				cmp.AllowUnexported(bkMultipartUpload{}),
				cmpopts.EquateComparable(uploader),
			); diff != "" {
				t.Fatalf("CreateWork diff (-got +want):\n%s", diff)
			}

			var gotEtags []api.ArtifactPartETag
			for _, wu := range work {
				etag, err := wu.DoWork(ctx)
				if err != nil {
					t.Errorf("bkUploaderWork.DoWork(ctx) = %v", err)
				}
				gotEtags = append(gotEtags, *etag)
			}

			wantEtags := []api.ArtifactPartETag{
				{PartNumber: 1, ETag: `"part number 1"`},
				{PartNumber: 2, ETag: `"part number 2"`},
				{PartNumber: 3, ETag: `"part number 3"`},
			}
			if diff := cmp.Diff(gotEtags, wantEtags); diff != "" {
				t.Errorf("etags diff (-got +want):\n%s", diff)
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
	t.Cleanup(func() {
		os.RemoveAll(temp) //nolint:errcheck // Best-effort cleanup.
	})

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
			},
		},
	}

	work, err := uploader.CreateWork(artifact)
	if err != nil {
		t.Fatalf("uploader.CreateWork(artifact) error = %v", err)
	}

	for _, wu := range work {
		if _, err := wu.DoWork(ctx); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("bkUploaderWork.DoWork(ctx) = %v, want %v", err, fs.ErrNotExist)
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
