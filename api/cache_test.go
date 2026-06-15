package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
)

func newTestCacheClient(t *testing.T, endpoint string) *api.Client {
	t.Helper()
	return api.NewClient(logger.Discard, api.Config{
		Endpoint:  endpoint,
		Token:     "test-token",
		UserAgent: "test-agent",
	})
}

func TestCacheEntryPeekExists_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want %q", r.Method, http.MethodPost)
		}
		if got, want := r.Header.Get("Authorization"), "Token test-token"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("User-Agent"), "test-agent"; got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(api.CacheEntryPeekResp{Message: "Cache exists"})
	}))
	defer server.Close()

	client := newTestCacheClient(t, server.URL)

	resp, exists, _, err := client.CacheEntryPeekExists(t.Context(), "test-slug", api.CacheEntryPeekReq{
		TargetPaths: []string{"node_modules"},
		CacheKey:    []api.CacheKeyPart{{Value: "test-key", Mandatory: true}},
	})
	if err != nil {
		t.Fatalf("CacheEntryPeekExists error = %v, want nil", err)
	}
	if !exists {
		t.Error("exists = false, want true")
	}
	if got, want := resp.Message, "Cache exists"; got != want {
		t.Errorf("resp.Message = %q, want %q", got, want)
	}
}

func TestCacheEntryPeekExists_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(api.CacheEntryPeekResp{Message: api.CacheEntryNotFound})
	}))
	defer server.Close()

	client := newTestCacheClient(t, server.URL)

	resp, exists, _, err := client.CacheEntryPeekExists(t.Context(), "test-slug", api.CacheEntryPeekReq{
		TargetPaths: []string{"node_modules"},
		CacheKey:    []api.CacheKeyPart{{Value: "nonexistent-key", Mandatory: true}},
	})
	if err != nil {
		t.Fatalf("CacheEntryPeekExists error = %v, want nil", err)
	}
	if exists {
		t.Error("exists = true, want false")
	}
	if got, want := resp.Message, api.CacheEntryNotFound; got != want {
		t.Errorf("resp.Message = %q, want %q", got, want)
	}
}

func TestCacheEntryPeekExists_WrongContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("plain text response"))
	}))
	defer server.Close()

	client := newTestCacheClient(t, server.URL)

	_, _, _, err := client.CacheEntryPeekExists(t.Context(), "test-slug", api.CacheEntryPeekReq{
		TargetPaths: []string{"node_modules"},
		CacheKey:    []api.CacheKeyPart{{Value: "test-key", Mandatory: true}},
	})
	if err == nil {
		t.Error("CacheEntryPeekExists error = nil, want non-nil for wrong content type")
	}
}

func TestCacheEntryPeekExists_CacheRegistryNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(api.CacheEntryPeekResp{Message: api.CacheRegistryNotFound})
	}))
	defer server.Close()

	client := newTestCacheClient(t, server.URL)

	_, _, _, err := client.CacheEntryPeekExists(t.Context(), "test-slug", api.CacheEntryPeekReq{
		TargetPaths: []string{"node_modules"},
		CacheKey:    []api.CacheKeyPart{{Value: "test-key", Mandatory: true}},
	})
	if err == nil {
		t.Error("CacheEntryPeekExists error = nil, want non-nil for cache registry not found")
	}
}

func TestCacheEntryPeekExists_ContentTypeWithCharset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(api.CacheEntryPeekResp{Message: "Cache exists"})
	}))
	defer server.Close()

	client := newTestCacheClient(t, server.URL)

	resp, exists, _, err := client.CacheEntryPeekExists(t.Context(), "test-slug", api.CacheEntryPeekReq{
		TargetPaths: []string{"node_modules"},
		CacheKey:    []api.CacheKeyPart{{Value: "test-key", Mandatory: true}},
	})
	if err != nil {
		t.Fatalf("CacheEntryPeekExists error = %v, want nil", err)
	}
	if !exists {
		t.Error("exists = false, want true")
	}
	if got, want := resp.Message, "Cache exists"; got != want {
		t.Errorf("resp.Message = %q, want %q", got, want)
	}
}

func TestCacheEntryCreate_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %q, want %q", r.Method, http.MethodPut)
		}
		var req api.CacheEntryCreateReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if len(req.CacheKey) != 1 || req.CacheKey[0].Value != "test-key" {
			t.Errorf("req.CacheKey = %+v, want single part with value %q", req.CacheKey, "test-key")
		}
		if len(req.Blobs) != 1 || req.Blobs[0].Digest.Value != "abc123" {
			t.Errorf("req.Blobs = %+v, want single blob with digest %q", req.Blobs, "abc123")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(api.CacheEntryCreateResp{
			UploadID:           "upload-123",
			Multipart:          false,
			UploadInstructions: []string{"curl -X PUT..."},
			Message:            "Created successfully",
		})
	}))
	defer server.Close()

	client := newTestCacheClient(t, server.URL)

	resp, _, err := client.CacheEntryCreate(t.Context(), "test-slug", api.CacheEntryCreateReq{
		TargetPaths: []string{"/path/1", "/path/2"},
		CacheKey:    []api.CacheKeyPart{{Value: "test-key", Mandatory: true}},
		Blobs: []api.CacheBlob{{
			Digest:      api.CacheDigest{Algorithm: "sha256", Value: "abc123"},
			FileSize:    1024,
			Compression: "zstd",
		}},
		Platform:     "linux/amd64",
		Pipeline:     "test-pipeline",
		Branch:       "main",
		Organization: "test-org",
	})
	if err != nil {
		t.Fatalf("CacheEntryCreate error = %v, want nil", err)
	}
	if got, want := resp.UploadID, "upload-123"; got != want {
		t.Errorf("resp.UploadID = %q, want %q", got, want)
	}
	if resp.Multipart {
		t.Error("resp.Multipart = true, want false")
	}
}

func TestCacheEntryRetrieve_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want %q", r.Method, http.MethodPost)
		}
		var req api.CacheEntryRetrieveReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if len(req.CacheKey) != 1 || req.CacheKey[0].Value != "test-key" {
			t.Errorf("req.CacheKey = %+v, want single part with value %q", req.CacheKey, "test-key")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(api.CacheEntryRetrieveResp{
			TargetPaths:          []string{"node_modules"},
			CacheKey:             []api.CacheKeyPart{{Value: "test-key", Mandatory: true}},
			Blobs:                []api.CacheBlob{{Digest: api.CacheDigest{Algorithm: "sha256", Value: "abc123"}, FileSize: 1024, Compression: "zstd"}},
			Fallback:             false,
			ExpiresAt:            time.Now().Add(24 * time.Hour),
			Multipart:            false,
			DownloadInstructions: []string{"curl -X GET..."},
			Message:              "Retrieved successfully",
		})
	}))
	defer server.Close()

	client := newTestCacheClient(t, server.URL)

	resp, found, _, err := client.CacheEntryRetrieve(t.Context(), "test-slug", api.CacheEntryRetrieveReq{
		TargetPaths: []string{"node_modules"},
		CacheKey:    []api.CacheKeyPart{{Value: "test-key", Mandatory: true}},
	})
	if err != nil {
		t.Fatalf("CacheEntryRetrieve error = %v, want nil", err)
	}
	if !found {
		t.Error("found = false, want true")
	}
	if len(resp.CacheKey) != 1 || resp.CacheKey[0].Value != "test-key" {
		t.Errorf("resp.CacheKey = %+v, want single part with value %q", resp.CacheKey, "test-key")
	}
	if resp.Fallback {
		t.Error("resp.Fallback = true, want false")
	}
}

func TestCacheEntryRetrieve_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(api.CacheEntryRetrieveResp{Message: api.CacheEntryNotFound})
	}))
	defer server.Close()

	client := newTestCacheClient(t, server.URL)

	resp, found, _, err := client.CacheEntryRetrieve(t.Context(), "test-slug", api.CacheEntryRetrieveReq{
		TargetPaths: []string{"node_modules"},
		CacheKey:    []api.CacheKeyPart{{Value: "nonexistent-key", Mandatory: true}},
	})
	if err != nil {
		t.Fatalf("CacheEntryRetrieve error = %v, want nil", err)
	}
	if found {
		t.Error("found = true, want false")
	}
	if got, want := resp.Message, api.CacheEntryNotFound; got != want {
		t.Errorf("resp.Message = %q, want %q", got, want)
	}
}

func TestCacheEntryCommit_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %q, want %q", r.Method, http.MethodPut)
		}
		var req api.CacheEntryCommitReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if got, want := req.UploadID, "upload-123"; got != want {
			t.Errorf("req.UploadID = %q, want %q", got, want)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(api.CacheEntryCommitResp{Message: "Committed successfully"})
	}))
	defer server.Close()

	client := newTestCacheClient(t, server.URL)

	resp, _, err := client.CacheEntryCommit(t.Context(), "test-slug", api.CacheEntryCommitReq{
		UploadID: "upload-123",
	})
	if err != nil {
		t.Fatalf("CacheEntryCommit error = %v, want nil", err)
	}
	if got, want := resp.Message, "Committed successfully"; got != want {
		t.Errorf("resp.Message = %q, want %q", got, want)
	}
}

func TestCacheEntryCommit_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(api.CacheEntryCommitResp{Message: "Invalid upload ID"})
	}))
	defer server.Close()

	client := newTestCacheClient(t, server.URL)

	_, _, err := client.CacheEntryCommit(t.Context(), "test-slug", api.CacheEntryCommitReq{
		UploadID: "invalid-upload-id",
	})
	if err == nil {
		t.Error("CacheEntryCommit error = nil, want non-nil for bad request")
	}
}
