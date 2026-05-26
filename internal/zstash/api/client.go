package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/internal/zstash/internal/trace"
	"github.com/google/go-querystring/query"
	"github.com/klauspost/compress/gzhttp"
	otel "go.opentelemetry.io/otel/trace"
)

const (
	CacheRegistryNotFound = "Cache registry not found"
	CacheEntryNotFound    = "Cache entry not found"
)

var ErrCacheEntryNotFound = errors.New("cache entry not found")

// CacheClient defines the interface for cache API operations.
// This interface is implemented by Client and can be mocked for testing.
type CacheClient interface {
	// CacheRegistry retrieves information about a cache registry.
	// Returns the registry configuration or an error if not found.
	CacheRegistry(ctx context.Context, registry string) (CacheRegistryResp, error)

	// CachePeekExists checks if a cache entry exists for the given key.
	// Returns the cache metadata, a boolean indicating existence (true if found), and any error.
	// When the cache entry is not found, returns (resp, false, nil) with resp.Message set.
	CachePeekExists(ctx context.Context, registry string, req CachePeekReq) (CachePeekResp, bool, error)

	// CacheCreate creates a new cache entry and returns upload information.
	// Returns upload instructions including the upload ID and storage location.
	CacheCreate(ctx context.Context, registry string, req CacheCreateReq) (CacheCreateResp, error)

	// CacheCommit marks a cache entry as committed after successful upload.
	// The upload ID from CacheCreate must be provided.
	CacheCommit(ctx context.Context, registry string, req CacheCommitReq) (CacheCommitResp, error)

	// CacheRetrieve retrieves download information for a cache entry.
	// Returns the cache metadata, a boolean indicating if found (true if exists), and any error.
	// When the cache entry is not found, returns (resp, false, nil) with resp.Message set.
	// The response may indicate a fallback key was used via resp.Fallback.
	CacheRetrieve(ctx context.Context, registry string, req CacheRetrieveReq) (CacheRetrieveResp, bool, error)
}

// Verify that Client implements CacheClient
var _ CacheClient = (*Client)(nil)

type Client struct {
	client   *http.Client
	endpoint string
}

type CacheCreateReq struct {
	Store        string   `json:"store"` // The store used for the cache entry
	Key          string   `json:"key"`
	FallbackKeys []string `json:"fallback_keys"`
	Compression  string   `json:"compression"`
	FileSize     int      `json:"file_size"`
	Digest       string   `json:"digest"`
	Paths        []string `json:"paths"`
	Platform     string   `json:"platform"`
	Pipeline     string   `json:"pipeline"`
	Branch       string   `json:"branch"`
	Organization string   `json:"owner"`
}

type CacheRetrieveReq struct {
	Key          string `url:"key"`
	Branch       string `url:"branch"`
	FallbackKeys string `url:"fallback_keys"`
}

type CacheRetrieveResp struct {
	Store                string    `json:"store"`             // The store used for the cache entry
	Key                  string    `json:"key"`               // The key of the cache entry, we MUST use this in rest of the restore process to cater for fallbacks
	Fallback             bool      `json:"fallback"`          // Indicates if this is a fallback cache entry
	StoreObjectName      string    `json:"store_object_name"` // the identifier used to read the key in blob storage
	ExpiresAt            time.Time `json:"expires_at"`
	CompressionType      string    `json:"compression_type"`
	Multipart            bool      `json:"multipart"`
	DownloadInstructions []string  `json:"download_instructions"`
	Message              string    `json:"message"`
}

type CacheCreateResp struct {
	UploadID           string   `json:"upload_id"` // the identifier used to write the key in blob storage
	StoreObjectName    string   `json:"store_object_name"`
	Multipart          bool     `json:"multipart"`
	UploadInstructions []string `json:"upload_instructions"`
	Message            string   `json:"message"`
}

type CachePeekReq struct {
	Key    string `url:"key"`
	Branch string `url:"branch"`
}

type CachePeekResp struct {
	Store        string    `json:"store"` // The store used for the cache entry
	Digest       string    `json:"digest"`
	ExpiresAt    time.Time `json:"expires_at"`
	Compression  string    `json:"compression"`
	Message      string    `json:"message"`
	FileSize     int       `json:"file_size"`
	Paths        []string  `json:"paths"`
	Pipeline     string    `json:"pipeline"`
	Branch       string    `json:"branch"`
	Owner        string    `json:"owner"`
	Platform     string    `json:"platform"`
	Key          string    `json:"key"`
	FallbackKeys []string  `json:"fallback_keys"`
	CreatedAt    time.Time `json:"created_at"`
	AgentID      string    `json:"agent_id"`
	JobID        string    `json:"job_id"`
	BuildID      string    `json:"build_id"`
}

type CacheRegistryResp struct {
	UUID  string `json:"uuid"`
	Name  string `json:"name"`
	Store string `json:"store"` // The store used for the cache registry
}

type CacheCommitReq struct {
	UploadID string `json:"upload_id"`
}
type CacheCommitResp struct {
	Message string `json:"message"`
}

func NewClient(ctx context.Context, version, endpoint, token string) Client {
	client := &http.Client{}

	client.Transport = gzhttp.Transport(roundTripperFunc(
		func(req *http.Request) (*http.Response, error) {
			req = req.Clone(req.Context())
			req.Header.Set("Authorization", fmt.Sprintf("Token %s", token))
			req.Header.Set("User-Agent", fmt.Sprint("zstash/", version))
			req.Header.Set("Accept", "application/json")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept-Encoding", "gzip, deflate, br")
			return http.DefaultTransport.RoundTrip(req)
		}),
	)

	return Client{client: client, endpoint: endpoint}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func (c Client) Do(req *http.Request) (*http.Response, error) {
	return c.client.Do(req) //nolint:gosec // G704: URL constructed from trusted endpoint set at client init
}

// MessageGetter interface for types that have a Message field
type MessageGetter interface {
	GetMessage() string
}

// GetMessage returns the message for CachePeekResp
func (r CachePeekResp) GetMessage() string {
	return r.Message
}

// GetMessage returns the message for CacheRetrieveResp
func (r CacheRetrieveResp) GetMessage() string {
	return r.Message
}

// handleCacheResponse handles common cache response patterns and error handling using generics
func handleCacheResponse[T MessageGetter](span otel.Span, res *http.Response, resp T) (T, bool, error) {
	// Assert content type is application/json for successful responses
	if res.StatusCode == http.StatusOK {
		contentType := res.Header.Get("Content-Type")
		if !isJSONContentType(contentType) {
			return resp, false, trace.NewError(span, "unexpected content type: %s", contentType)
		}
		return resp, true, nil
	}

	switch res.StatusCode {
	case http.StatusNotFound:
		switch resp.GetMessage() {
		case CacheEntryNotFound:
			return resp, false, nil
		case CacheRegistryNotFound:
			return resp, false, trace.NewError(span, "cache registry not found: %s", res.Status)
		}
		return resp, false, trace.NewError(span, "not found: %s", res.Status)
	case http.StatusBadRequest:
		return resp, false, trace.NewError(span, "bad request: %s", res.Status)
	default:
		return resp, false, trace.NewError(span, "request failed with status: %s", res.Status)
	}
}

func (c Client) CacheRegistry(ctx context.Context, registry string) (CacheRegistryResp, error) {
	ctx, span := trace.Start(ctx, "Client.CacheRegistry")
	defer span.End()

	var resp CacheRegistryResp

	u, err := url.Parse(fmt.Sprintf("%s/cache_registries/%s", c.endpoint, registry))
	if err != nil {
		return resp, trace.NewError(span, "failed to parse url: %w", err)
	}

	res, resp, err := doRequest[any, CacheRegistryResp](ctx, c.client, http.MethodGet, u.String(), nil)
	if err != nil {
		return resp, trace.NewError(span, "failed to do request: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		return resp, trace.NewError(span, "failed to get cache registry: %s", res.Status)
	}

	// Assert content type is application/json
	contentType := res.Header.Get("Content-Type")
	if !isJSONContentType(contentType) {
		return resp, trace.NewError(span, "unexpected content type: %s", contentType)
	}

	return resp, nil
}

func (c Client) CachePeekExists(ctx context.Context, registry string, create CachePeekReq) (CachePeekResp, bool, error) {
	ctx, span := trace.Start(ctx, "Client.CachePeekExists")
	defer span.End()

	var resp CachePeekResp

	queryParams, err := query.Values(create)
	if err != nil {
		return resp, false, trace.NewError(span, "failed to marshal query params: %w", err)
	}

	u, err := url.Parse(fmt.Sprintf("%s/cache_registries/%s/peek", c.endpoint, registry))
	if err != nil {
		return resp, false, trace.NewError(span, "failed to parse url: %w", err)
	}

	u.RawQuery = queryParams.Encode()

	res, resp, err := doRequest[any, CachePeekResp](ctx, c.client, http.MethodGet, u.String(), nil)
	if err != nil {
		return resp, false, trace.NewError(span, "failed to do request: %w", err)
	}

	return handleCacheResponse(span, res, resp)
}

func (c Client) CacheCommit(ctx context.Context, registry string, commit CacheCommitReq) (CacheCommitResp, error) {
	ctx, span := trace.Start(ctx, "Client.CacheCommit")
	defer span.End()

	var resp CacheCommitResp

	u, err := url.Parse(fmt.Sprintf("%s/cache_registries/%s/commit", c.endpoint, registry))
	if err != nil {
		return resp, trace.NewError(span, "failed to parse url: %w", err)
	}

	res, resp, err := doRequest[CacheCommitReq, CacheCommitResp](ctx, c.client, http.MethodPut, u.String(), &commit)
	if err != nil {
		return resp, trace.NewError(span, "failed to do request: %w", err)
	}

	slog.Debug("Cache committed with the following parameters", "resp", resp)

	if res.StatusCode != http.StatusOK {
		return resp, trace.NewError(span, "failed to commit: %s", res.Status)
	}

	return resp, nil
}

func (c Client) CacheCreate(ctx context.Context, registry string, create CacheCreateReq) (CacheCreateResp, error) {
	ctx, span := trace.Start(ctx, "Client.CacheCreate")
	defer span.End()

	var resp CacheCreateResp

	u, err := url.Parse(fmt.Sprintf("%s/cache_registries/%s/store", c.endpoint, registry))
	if err != nil {
		return resp, trace.NewError(span, "failed to parse url: %w", err)
	}

	res, resp, err := doRequest[CacheCreateReq, CacheCreateResp](ctx, c.client, http.MethodPut, u.String(), &create)
	if err != nil {
		return resp, trace.NewError(span, "failed to do request: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		return resp, trace.NewError(span, "failed to save: %s", res.Status)
	}

	return resp, nil
}

func (c Client) CacheRetrieve(ctx context.Context, registry string, retrieve CacheRetrieveReq) (CacheRetrieveResp, bool, error) {
	ctx, span := trace.Start(ctx, "Client.CacheRetrieve")
	defer span.End()

	var resp CacheRetrieveResp

	queryParams, err := query.Values(retrieve)
	if err != nil {
		return resp, false, trace.NewError(span, "failed to marshal query params: %w", err)
	}

	u, err := url.Parse(fmt.Sprintf("%s/cache_registries/%s/retrieve", c.endpoint, registry))
	if err != nil {
		return resp, false, trace.NewError(span, "failed to parse url: %w", err)
	}

	u.RawQuery = queryParams.Encode()

	slog.Debug("Cache retrieve URL", "url", u.String())

	res, resp, err := doRequest[CacheRetrieveReq, CacheRetrieveResp](ctx, c.client, http.MethodGet, u.String(), nil)
	if err != nil {
		return resp, false, trace.NewError(span, "failed to do request: %w", err)
	}

	slog.Debug("Cache retrieved with the following parameters",
		"resp", resp,
		"status", res.Status,
		"code", res.StatusCode)

	return handleCacheResponse(span, res, resp)
}

func doRequest[T, V any](ctx context.Context, client *http.Client, method, url string, body *T) (res *http.Response, resp V, err error) {
	ctx, span := trace.Start(ctx, "DoRequest")
	defer span.End()

	var bodyrdr io.Reader = http.NoBody

	// ONLY set body if method is PUT or POST
	if method == http.MethodPut || method == http.MethodPost {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, resp, trace.NewError(span, "failed to marshal request body: %w", err)
		}
		bodyrdr = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyrdr)
	if err != nil {
		return nil, resp, trace.NewError(span, "failed to create request: %w", err)
	}

	res, err = client.Do(req)
	if err != nil {
		return nil, resp, trace.NewError(span, "failed to do request: %w", err)
	}

	// Don't return error for expected status codes that are handled by callers
	if res.StatusCode < 200 || res.StatusCode >= 500 {
		return res, resp, trace.NewError(span, "request failed with status: %s", res.Status)
	}

	if res.Body == http.NoBody {
		return res, resp, nil
	}

	defer func() {
		if res != nil && res.Body != nil {
			_ = res.Body.Close()
		}
	}()

	// Assert content type is application/json
	contentType := res.Header.Get("Content-Type")
	if !isJSONContentType(contentType) {
		return res, resp, trace.NewError(span, "unexpected content type: %s", contentType)
	}

	// read the response body
	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, resp, trace.NewError(span, "failed to read response body: %w", err)
	}

	slog.Debug("API call", "method", method, "url", url, "status", res.StatusCode, "body", string(respBody))

	if err = json.Unmarshal(respBody, &resp); err != nil {
		return nil, resp, trace.NewError(span, "failed to decode response body: %w", err)
	}

	return res, resp, nil
}

// isJSONContentType checks if the content type indicates JSON response
// Handles cases like "application/json", "application/json; charset=utf-8",
// and structured syntax suffixes like "application/problem+json"
func isJSONContentType(contentType string) bool {
	// Remove any leading/trailing whitespace and convert to lowercase
	contentType = strings.TrimSpace(strings.ToLower(contentType))

	// Remove any parameters (e.g., "; charset=utf-8")
	if idx := strings.Index(contentType, ";"); idx != -1 {
		contentType = strings.TrimSpace(contentType[:idx])
	}

	// Check if it's application/json or application/*+json (e.g., application/problem+json)
	return contentType == "application/json" || strings.HasPrefix(contentType, "application/") && strings.HasSuffix(contentType, "+json")
}
