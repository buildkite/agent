package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/internal/agenthttp"
	"github.com/google/go-querystring/query"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// Cache API "not found" messages. The cache service returns HTTP 404 with one
// of these messages in the JSON body to indicate semantically-distinct cases.
const (
	CacheRegistryNotFound = "Cache registry not found"
	CacheEntryNotFound    = "Cache entry not found"
)

// ErrCacheEntryNotFound is reserved for callers that want a sentinel for the
// "no entry" condition. The cache methods themselves report it via the
// (resp, exists, err) return shape; this value is exported for parity.
var ErrCacheEntryNotFound = errors.New("cache entry not found")

var cacheTracer = otel.Tracer("github.com/buildkite/agent/v3/api/cache")

// CacheCreateReq is the request body for creating a cache entry.
type CacheCreateReq struct {
	Store        string   `json:"store"`
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

// CacheRetrieveReq is the query for retrieving a cache entry.
type CacheRetrieveReq struct {
	Key          string `url:"key"`
	Branch       string `url:"branch"`
	FallbackKeys string `url:"fallback_keys"`
}

// CacheRetrieveResp describes the cache entry to download.
type CacheRetrieveResp struct {
	Store                string    `json:"store"`
	Key                  string    `json:"key"`
	Fallback             bool      `json:"fallback"`
	StoreObjectName      string    `json:"store_object_name"`
	ExpiresAt            time.Time `json:"expires_at"`
	CompressionType      string    `json:"compression_type"`
	Multipart            bool      `json:"multipart"`
	DownloadInstructions []string  `json:"download_instructions"`
	Message              string    `json:"message"`
}

// CacheCreateResp describes where and how to upload the new cache entry.
type CacheCreateResp struct {
	UploadID           string   `json:"upload_id"`
	StoreObjectName    string   `json:"store_object_name"`
	Multipart          bool     `json:"multipart"`
	UploadInstructions []string `json:"upload_instructions"`
	Message            string   `json:"message"`
}

// CachePeekReq is the query for checking whether a cache entry exists.
type CachePeekReq struct {
	Key    string `url:"key"`
	Branch string `url:"branch"`
}

// CachePeekResp describes the cache entry returned by a peek.
type CachePeekResp struct {
	Store        string    `json:"store"`
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

// CacheRegistryResp describes a configured cache registry.
type CacheRegistryResp struct {
	UUID  string `json:"uuid"`
	Name  string `json:"name"`
	Store string `json:"store"`
}

// CacheCommitReq is the request body for committing a previously created cache entry.
type CacheCommitReq struct {
	UploadID string `json:"upload_id"`
}

// CacheCommitResp acknowledges a commit.
type CacheCommitResp struct {
	Message string `json:"message"`
}

// CacheRegistry retrieves information about a cache registry.
func (c *Client) CacheRegistry(ctx context.Context, registry string) (CacheRegistryResp, error) {
	ctx, span := cacheTracer.Start(ctx, "Client.CacheRegistry")
	defer span.End()

	var resp CacheRegistryResp

	req, err := c.newRequest(ctx, http.MethodGet, cachePath("/cache_registries/%s", registry), nil)
	if err != nil {
		return resp, cacheSpanErr(span, "failed to create request: %w", err)
	}

	res, err := c.cacheDo(req, &resp)
	if err != nil {
		return resp, cacheSpanErr(span, "%w", err)
	}
	if res.StatusCode != http.StatusOK {
		return resp, cacheSpanErr(span, "failed to get cache registry: %s", res.Status)
	}
	return resp, nil
}

// CachePeekExists checks whether a cache entry exists.
// Returns (resp, true, nil) on hit, (resp, false, nil) on miss (HTTP 404 with
// CacheEntryNotFound), or (resp, false, err) on any other failure.
func (c *Client) CachePeekExists(ctx context.Context, registry string, peek CachePeekReq) (CachePeekResp, bool, error) {
	ctx, span := cacheTracer.Start(ctx, "Client.CachePeekExists")
	defer span.End()

	var resp CachePeekResp

	path, err := cacheQueryPath("/cache_registries/%s/peek", registry, peek)
	if err != nil {
		return resp, false, cacheSpanErr(span, "%w", err)
	}

	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return resp, false, cacheSpanErr(span, "failed to create request: %w", err)
	}

	res, err := c.cacheDo(req, &resp)
	if err != nil {
		return resp, false, cacheSpanErr(span, "%w", err)
	}
	return interpretCacheResponse(span, res, resp)
}

// CacheCreate creates a new cache entry and returns upload instructions.
func (c *Client) CacheCreate(ctx context.Context, registry string, create CacheCreateReq) (CacheCreateResp, error) {
	ctx, span := cacheTracer.Start(ctx, "Client.CacheCreate")
	defer span.End()

	var resp CacheCreateResp

	req, err := c.newRequest(ctx, http.MethodPut, cachePath("/cache_registries/%s/store", registry), &create)
	if err != nil {
		return resp, cacheSpanErr(span, "failed to create request: %w", err)
	}

	res, err := c.cacheDo(req, &resp)
	if err != nil {
		return resp, cacheSpanErr(span, "%w", err)
	}
	if res.StatusCode != http.StatusOK {
		return resp, cacheSpanErr(span, "failed to save: %s", res.Status)
	}
	return resp, nil
}

// CacheCommit marks a previously created cache entry as committed.
func (c *Client) CacheCommit(ctx context.Context, registry string, commit CacheCommitReq) (CacheCommitResp, error) {
	ctx, span := cacheTracer.Start(ctx, "Client.CacheCommit")
	defer span.End()

	var resp CacheCommitResp

	req, err := c.newRequest(ctx, http.MethodPut, cachePath("/cache_registries/%s/commit", registry), &commit)
	if err != nil {
		return resp, cacheSpanErr(span, "failed to create request: %w", err)
	}

	res, err := c.cacheDo(req, &resp)
	if err != nil {
		return resp, cacheSpanErr(span, "%w", err)
	}
	if res.StatusCode != http.StatusOK {
		return resp, cacheSpanErr(span, "failed to commit: %s", res.Status)
	}
	return resp, nil
}

// CacheRetrieve retrieves download instructions for a cache entry.
// Returns (resp, true, nil) on hit (possibly via a fallback key), (resp, false,
// nil) on miss, or (resp, false, err) on any other failure.
func (c *Client) CacheRetrieve(ctx context.Context, registry string, retrieve CacheRetrieveReq) (CacheRetrieveResp, bool, error) {
	ctx, span := cacheTracer.Start(ctx, "Client.CacheRetrieve")
	defer span.End()

	var resp CacheRetrieveResp

	path, err := cacheQueryPath("/cache_registries/%s/retrieve", registry, retrieve)
	if err != nil {
		return resp, false, cacheSpanErr(span, "%w", err)
	}

	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return resp, false, cacheSpanErr(span, "failed to create request: %w", err)
	}

	res, err := c.cacheDo(req, &resp)
	if err != nil {
		return resp, false, cacheSpanErr(span, "%w", err)
	}
	return interpretCacheResponse(span, res, resp)
}

// cachePath formats a cache API path with URL-safe escaping for path components.
func cachePath(format string, args ...any) string {
	escaped := make([]any, len(args))
	for i, a := range args {
		escaped[i] = url.PathEscape(fmt.Sprint(a))
	}
	return fmt.Sprintf(format, escaped...)
}

// cacheQueryPath formats a cache API path and appends url-tagged query params.
func cacheQueryPath(format, registry string, params any) (string, error) {
	q, err := query.Values(params)
	if err != nil {
		return "", fmt.Errorf("failed to marshal query params: %w", err)
	}
	return cachePath(format, registry) + "?" + q.Encode(), nil
}

// cacheDo dispatches req through the agent HTTP stack (debug-HTTP, trace-HTTP)
// and decodes a JSON body into resp. Unlike Client.doRequest, it does not
// treat non-2xx as an error — cache responses use 404 + a message to signal a
// miss, and callers want to inspect the status themselves.
func (c *Client) cacheDo(req *http.Request, resp any) (*http.Response, error) {
	httpResp, err := agenthttp.Do(c.logger, c.client, req,
		agenthttp.WithDebugHTTP(c.conf.DebugHTTP),
		agenthttp.WithTraceHTTP(c.conf.TraceHTTP),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to do request: %w", err)
	}
	defer httpResp.Body.Close()              //nolint:errcheck
	defer io.Copy(io.Discard, httpResp.Body) //nolint:errcheck

	if httpResp.StatusCode >= 500 {
		return httpResp, fmt.Errorf("request failed with status: %s", httpResp.Status)
	}
	if httpResp.Body == http.NoBody {
		return httpResp, nil
	}

	contentType := httpResp.Header.Get("Content-Type")
	if !isJSONContent(contentType) {
		return httpResp, fmt.Errorf("unexpected content type: %s", contentType)
	}

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return httpResp, fmt.Errorf("failed to read response body: %w", err)
	}
	if len(body) == 0 {
		return httpResp, nil
	}
	if err := json.Unmarshal(body, resp); err != nil {
		return httpResp, fmt.Errorf("failed to decode response body: %w", err)
	}
	return httpResp, nil
}

// cacheMessage is the subset of cache responses needed to classify a 404.
type cacheMessage interface {
	cacheMessage() string
}

func (r CachePeekResp) cacheMessage() string     { return r.Message }
func (r CacheRetrieveResp) cacheMessage() string { return r.Message }

// interpretCacheResponse maps the dual "200 = hit, 404 + message = miss"
// convention into the (resp, exists, err) return shape used by peek/retrieve.
func interpretCacheResponse[T cacheMessage](span oteltrace.Span, res *http.Response, resp T) (T, bool, error) {
	if res.StatusCode == http.StatusOK {
		return resp, true, nil
	}
	switch res.StatusCode {
	case http.StatusNotFound:
		switch resp.cacheMessage() {
		case CacheEntryNotFound:
			return resp, false, nil
		case CacheRegistryNotFound:
			return resp, false, cacheSpanErr(span, "cache registry not found: %s", res.Status)
		}
		return resp, false, cacheSpanErr(span, "not found: %s", res.Status)
	case http.StatusBadRequest:
		return resp, false, cacheSpanErr(span, "bad request: %s", res.Status)
	default:
		return resp, false, cacheSpanErr(span, "request failed with status: %s", res.Status)
	}
}

func cacheSpanErr(span oteltrace.Span, format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	return err
}

// isJSONContent reports whether contentType represents JSON, including media
// types with suffix (e.g. application/problem+json) or parameters (e.g.
// application/json; charset=utf-8).
func isJSONContent(contentType string) bool {
	contentType = strings.TrimSpace(strings.ToLower(contentType))
	if i := strings.Index(contentType, ";"); i != -1 {
		contentType = strings.TrimSpace(contentType[:i])
	}
	return contentType == "application/json" ||
		strings.HasPrefix(contentType, "application/") && strings.HasSuffix(contentType, "+json")
}
