package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	client := NewClient(context.Background(), "1.0.0", "https://api.example.com", "test-token")

	if client.endpoint != "https://api.example.com" {
		t.Errorf("Expected endpoint 'https://api.example.com', got '%s'", client.endpoint)
	}

	if client.client == nil {
		t.Error("Expected client to be initialized")
	}
}

func TestCachePeekExists_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET method, got %s", r.Method)
		}

		if r.Header.Get("Authorization") != "Token test-token" {
			t.Errorf("Expected Authorization header 'Token test-token', got '%s'", r.Header.Get("Authorization"))
		}

		if r.Header.Get("User-Agent") != "zstash/1.0.0" {
			t.Errorf("Expected User-Agent 'zstash/1.0.0', got '%s'", r.Header.Get("User-Agent"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(CachePeekResp{Message: "Cache exists"})
	}))
	defer server.Close()

	client := NewClient(context.Background(), "1.0.0", server.URL, "test-token")

	req := CachePeekReq{
		Key:    "test-key",
		Branch: "main",
	}

	resp, exists, err := client.CachePeekExists(context.Background(), "test-slug", req)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if !exists {
		t.Error("Expected cache to exist")
	}

	if resp.Message != "Cache exists" {
		t.Errorf("Expected message 'Cache exists', got '%s'", resp.Message)
	}
}

func TestCachePeekExists_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(CachePeekResp{Message: CacheEntryNotFound})
	}))
	defer server.Close()

	client := NewClient(context.Background(), "1.0.0", server.URL, "test-token")

	req := CachePeekReq{
		Key:    "nonexistent-key",
		Branch: "main",
	}

	resp, exists, err := client.CachePeekExists(context.Background(), "test-slug", req)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if exists {
		t.Error("Expected cache to not exist")
	}

	if resp.Message != CacheEntryNotFound {
		t.Errorf("Expected message '%s', got '%s'", CacheEntryNotFound, resp.Message)
	}
}

func TestCachePeekExists_WrongContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("plain text response"))
	}))
	defer server.Close()

	client := NewClient(context.Background(), "1.0.0", server.URL, "test-token")

	req := CachePeekReq{
		Key:    "test-key",
		Branch: "main",
	}

	_, _, err := client.CachePeekExists(context.Background(), "test-slug", req)
	if err == nil {
		t.Error("Expected error for wrong content type")
	}
}

func TestCacheCreate_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("Expected PUT method, got %s", r.Method)
		}

		var req CacheCreateReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}

		if req.Key != "test-key" {
			t.Errorf("Expected key 'test-key', got '%s'", req.Key)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(CacheCreateResp{
			UploadID:           "upload-123",
			Multipart:          false,
			UploadInstructions: []string{"curl -X PUT..."},
			Message:            "Created successfully",
		})
	}))
	defer server.Close()

	client := NewClient(context.Background(), "1.0.0", server.URL, "test-token")

	req := CacheCreateReq{
		Key:          "test-key",
		FallbackKeys: []string{"fallback-1", "fallback-2"},
		Compression:  "gzip",
		FileSize:     1024,
		Digest:       "sha256:abc123",
		Paths:        []string{"/path/1", "/path/2"},
		Platform:     "linux",
		Pipeline:     "test-pipeline",
		Branch:       "main",
		Organization: "test-org",
	}

	resp, err := client.CacheCreate(context.Background(), "test-slug", req)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if resp.UploadID != "upload-123" {
		t.Errorf("Expected upload ID 'upload-123', got '%s'", resp.UploadID)
	}

	if resp.Multipart {
		t.Error("Expected multipart to be false")
	}
}

func TestCacheRetrieve_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET method, got %s", r.Method)
		}

		// Check query parameters
		if r.URL.Query().Get("key") != "test-key" {
			t.Errorf("Expected key query param 'test-key', got '%s'", r.URL.Query().Get("key"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(CacheRetrieveResp{
			Key:                  "test-key",
			Fallback:             false,
			ExpiresAt:            time.Now().Add(24 * time.Hour),
			Multipart:            false,
			DownloadInstructions: []string{"curl -X GET..."},
			Message:              "Retrieved successfully",
		})
	}))
	defer server.Close()

	client := NewClient(context.Background(), "1.0.0", server.URL, "test-token")

	req := CacheRetrieveReq{
		Key:          "test-key",
		Branch:       "main",
		FallbackKeys: "fallback-1,fallback-2",
	}

	resp, found, err := client.CacheRetrieve(context.Background(), "test-slug", req)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if !found {
		t.Error("Expected cache to be found")
	}

	if resp.Key != "test-key" {
		t.Errorf("Expected key 'test-key', got '%s'", resp.Key)
	}

	if resp.Fallback {
		t.Error("Expected fallback to be false")
	}
}

func TestCacheRetrieve_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(CacheRetrieveResp{Message: CacheEntryNotFound})
	}))
	defer server.Close()

	client := NewClient(context.Background(), "1.0.0", server.URL, "test-token")

	req := CacheRetrieveReq{
		Key:    "nonexistent-key",
		Branch: "main",
	}

	resp, found, err := client.CacheRetrieve(context.Background(), "test-slug", req)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if found {
		t.Error("Expected cache to not be found")
	}

	if resp.Message != CacheEntryNotFound {
		t.Errorf("Expected message '%s', got '%s'", CacheEntryNotFound, resp.Message)
	}
}

func TestCacheCommit_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("Expected PUT method, got %s", r.Method)
		}

		var req CacheCommitReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}

		if req.UploadID != "upload-123" {
			t.Errorf("Expected upload ID 'upload-123', got '%s'", req.UploadID)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(CacheCommitResp{Message: "Committed successfully"})
	}))
	defer server.Close()

	client := NewClient(context.Background(), "1.0.0", server.URL, "test-token")

	req := CacheCommitReq{
		UploadID: "upload-123",
	}

	resp, err := client.CacheCommit(context.Background(), "test-slug", req)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if resp.Message != "Committed successfully" {
		t.Errorf("Expected message 'Committed successfully', got '%s'", resp.Message)
	}
}

func TestCacheCommit_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(CacheCommitResp{Message: "Invalid upload ID"})
	}))
	defer server.Close()

	client := NewClient(context.Background(), "1.0.0", server.URL, "test-token")

	req := CacheCommitReq{
		UploadID: "invalid-upload-id",
	}

	_, err := client.CacheCommit(context.Background(), "test-slug", req)
	if err == nil {
		t.Error("Expected error for bad request")
	}
}

func TestCachePeekExists_CacheRegistryNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(CachePeekResp{Message: CacheRegistryNotFound})
	}))
	defer server.Close()

	client := NewClient(context.Background(), "1.0.0", server.URL, "test-token")

	req := CachePeekReq{
		Key:    "test-key",
		Branch: "main",
	}

	_, _, err := client.CachePeekExists(context.Background(), "test-slug", req)
	if err == nil {
		t.Error("Expected error for cache registry not found")
	}
}

func TestDoRequest_NoBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET method, got %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "success"})
	}))
	defer server.Close()

	client := &http.Client{}

	type testResp struct {
		Message string `json:"message"`
	}

	res, resp, err := doRequest[any, testResp](context.Background(), client, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if res.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", res.StatusCode)
	}

	if resp.Message != "success" {
		t.Errorf("Expected message 'success', got '%s'", resp.Message)
	}
}

func TestDoRequest_WithBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("Expected PUT method, got %s", r.Method)
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}

		if body["test"] != "value" {
			t.Errorf("Expected body test field 'value', got '%s'", body["test"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "success"})
	}))
	defer server.Close()

	client := &http.Client{}

	type testReq struct {
		Test string `json:"test"`
	}

	type testResp struct {
		Message string `json:"message"`
	}

	reqBody := testReq{Test: "value"}

	res, resp, err := doRequest[testReq, testResp](context.Background(), client, http.MethodPut, server.URL, &reqBody)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if res.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", res.StatusCode)
	}

	if resp.Message != "success" {
		t.Errorf("Expected message 'success', got '%s'", resp.Message)
	}
}

func TestRoundTripperFunc(t *testing.T) {
	called := false
	fn := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		// Return a basic response
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       http.NoBody,
		}, nil
	})

	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	_, err := fn.RoundTrip(req)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if !called {
		t.Error("Expected round tripper function to be called")
	}
}

func TestCachePeekExists_ContentTypeWithCharset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(CachePeekResp{Message: "Cache exists"})
	}))
	defer server.Close()

	client := NewClient(context.Background(), "1.0.0", server.URL, "test-token")

	req := CachePeekReq{
		Key:    "test-key",
		Branch: "main",
	}

	resp, exists, err := client.CachePeekExists(context.Background(), "test-slug", req)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if !exists {
		t.Error("Expected cache to exist")
	}

	if resp.Message != "Cache exists" {
		t.Errorf("Expected message 'Cache exists', got '%s'", resp.Message)
	}
}

func TestIsJSONContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		expected    bool
	}{
		{"basic JSON", "application/json", true},
		{"JSON with charset", "application/json; charset=utf-8", true},
		{"JSON with charset uppercase", "APPLICATION/JSON; CHARSET=UTF-8", true},
		{"JSON with additional params", "application/json; charset=utf-8; boundary=something", true},
		{"JSON with spaces", "  application/json  ", true},
		{"JSON with spaces and charset", "  application/json; charset=utf-8  ", true},
		{"text plain", "text/plain", false},
		{"HTML", "text/html", false},
		{"XML", "application/xml", false},
		{"empty", "", false},
		{"partial match", "json", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isJSONContentType(tt.contentType)
			if result != tt.expected {
				t.Errorf("isJSONContentType(%q) = %v, expected %v", tt.contentType, result, tt.expected)
			}
		})
	}
}
