package api_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
)

func TestUploadChunk(t *testing.T) {
	t.Parallel()
	const data = "☃\x1b]7501;state=working:progress=0\a\r\n"
	const sidecar = `{"version":1,"events":[{"log_byte_offset":3,"log_byte_end":35,"record":{"id":"","state":"working","progress":0}}]}`
	for _, events := range []string{"", sidecar} {
		t.Run(events, func(t *testing.T) {
			var compressedSize int
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/v3/jobs/job-id/chunks" || r.URL.RawQuery != "sequence=2&offset=0&size=37" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				if got := r.Header.Get("Authorization"); got != "Token job-token" {
					t.Errorf("Authorization = %q", got)
				}
				var compressed io.Reader = r.Body
				if events == "" {
					if r.Header.Get("Content-Type") != "text/plain" || r.Header.Get("Content-Encoding") != "gzip" {
						t.Errorf("incorrect direct-gzip headers: %v", r.Header)
					}
				} else {
					if r.Header.Get("Content-Encoding") != "" {
						t.Error("multipart body must not be marked gzip")
					}
					if err := r.ParseMultipartForm(1 << 20); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					defer func() {
						if err := r.MultipartForm.RemoveAll(); err != nil {
							t.Error(err)
						}
					}()
					if got := r.FormValue("execution_events"); got != events {
						t.Errorf("sidecar = %q, want %q", got, events)
					}
					file, header, err := r.FormFile("chunk")
					if err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					defer func() {
						if err := file.Close(); err != nil {
							t.Error(err)
						}
					}()
					if header.Filename != "chunk.gz" {
						t.Errorf("filename = %q, want chunk.gz", header.Filename)
					}
					compressed = file
				}
				body, err := io.ReadAll(compressed)
				if err != nil {
					t.Error(err)
				}
				compressedSize = len(body)
				reader, err := gzip.NewReader(bytes.NewReader(body))
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				defer func() {
					if err := reader.Close(); err != nil {
						t.Error(err)
					}
				}()
				got, err := io.ReadAll(reader)
				if err != nil || string(got) != data {
					t.Errorf("decompressed log = %q, %v; want %q", got, err, data)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":"chunk-id"}`))
			}))
			defer server.Close()
			client := api.NewClient(logger.Discard, api.Config{Endpoint: server.URL + "/v3", Token: "job-token"})
			chunk := &api.Chunk{Data: []byte(data), Sequence: 2, Size: uint64(len(data)), ExecutionEvents: []byte(events)}
			// Retry the same chunk. The metadata and decompressed bytes must match.
			for range 2 {
				if _, err := client.UploadChunk(t.Context(), "job-id", chunk); err != nil {
					t.Fatal(err)
				}
				if chunk.CompressedBytes != uint64(compressedSize) {
					t.Errorf("CompressedBytes = %d, want %d", chunk.CompressedBytes, compressedSize)
				}
			}
			if calls != 2 {
				t.Errorf("got %d requests, want 2", calls)
			}
		})
	}
}
