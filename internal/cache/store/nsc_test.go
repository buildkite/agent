package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	storagev1beta "buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/cloud/storage/v1beta"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"namespacelabs.dev/integrations/api/storage"
)

func TestNscStore_Interface(t *testing.T) {
	var _ Blob = (*NscStore)(nil)
}

type fakeNscAPIClient struct {
	upload     func(context.Context, string, string, io.Reader, storage.UploadOpts) error
	resolve    func(context.Context, string, string) (io.ReadCloser, error)
	extend     func(context.Context, *storagev1beta.ExtendArtifactRequest) error
	close      func() error
	closeCalls atomic.Int32
}

func (c *fakeNscAPIClient) UploadArtifact(ctx context.Context, nsc, path string, r io.Reader, opts storage.UploadOpts) error {
	if c.upload == nil {
		return nil
	}
	return c.upload(ctx, nsc, path, r, opts)
}

func (c *fakeNscAPIClient) ResolveArtifactStream(ctx context.Context, nsc, path string) (io.ReadCloser, error) {
	if c.resolve == nil {
		return io.NopCloser(strings.NewReader("")), nil
	}
	return c.resolve(ctx, nsc, path)
}

func (c *fakeNscAPIClient) ExtendArtifact(ctx context.Context, req *storagev1beta.ExtendArtifactRequest) error {
	if c.extend == nil {
		return nil
	}
	return c.extend(ctx, req)
}

func (c *fakeNscAPIClient) Close() error {
	c.closeCalls.Add(1)
	if c.close == nil {
		return nil
	}
	return c.close()
}

func TestNscClient_IsLazy(t *testing.T) {
	var initializeCalls atomic.Int32
	client := newNscClient(func(context.Context) (nscAPIClient, error) {
		initializeCalls.Add(1)
		return &fakeNscAPIClient{}, nil
	})

	if got := initializeCalls.Load(); got != 0 {
		t.Fatalf("initialization calls after construction = %d, want 0", got)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := initializeCalls.Load(); got != 0 {
		t.Fatalf("initialization calls after closing unused client = %d, want 0", got)
	}
}

func TestNscClient_ConcurrentInitialization(t *testing.T) {
	var initializeCalls atomic.Int32
	want := &fakeNscAPIClient{}
	client := newNscClient(func(context.Context) (nscAPIClient, error) {
		initializeCalls.Add(1)
		return want, nil
	})

	const callers = 32
	results := make(chan nscAPIClient, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := client.get(t.Context())
			results <- got
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("get: %v", err)
		}
	}
	for got := range results {
		if got != want {
			t.Errorf("get returned %p, want %p", got, want)
		}
	}
	if got := initializeCalls.Load(); got != 1 {
		t.Errorf("initialization calls = %d, want 1", got)
	}
}

func TestNscClient_InitializationError(t *testing.T) {
	var initializeCalls atomic.Int32
	wantErr := errors.New("initialization failed")
	client := newNscClient(func(context.Context) (nscAPIClient, error) {
		initializeCalls.Add(1)
		return nil, wantErr
	})

	for range 2 {
		got, err := client.get(t.Context())
		if got != nil {
			t.Errorf("get returned client %v, want nil", got)
		}
		if !errors.Is(err, wantErr) {
			t.Errorf("get error = %v, want %v", err, wantErr)
		}
	}
	if got := initializeCalls.Load(); got != 1 {
		t.Errorf("initialization calls = %d, want 1", got)
	}
}

func TestNscClient_CloseOnce(t *testing.T) {
	want := &fakeNscAPIClient{}
	client := newNscClient(func(context.Context) (nscAPIClient, error) {
		return want, nil
	})
	if _, err := client.get(t.Context()); err != nil {
		t.Fatalf("get: %v", err)
	}

	for range 2 {
		if err := client.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
	if got := want.closeCalls.Load(); got != 1 {
		t.Errorf("close calls = %d, want 1", got)
	}
}

func TestNscClient_CloseError(t *testing.T) {
	wantErr := errors.New("close failed")
	want := &fakeNscAPIClient{close: func() error { return wantErr }}
	client := newNscClient(func(context.Context) (nscAPIClient, error) {
		return want, nil
	})
	if _, err := client.get(t.Context()); err != nil {
		t.Fatalf("get: %v", err)
	}

	for range 2 {
		if err := client.Close(); !errors.Is(err, wantErr) {
			t.Fatalf("Close error = %v, want %v", err, wantErr)
		}
	}
	if got := want.closeCalls.Load(); got != 1 {
		t.Errorf("close calls = %d, want 1", got)
	}
}

func TestParseNscNamespace(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		nsc     string
		wantErr bool
	}{
		{name: "nsc with namespace", url: "nsc://my-namespace", nsc: "my-namespace"},
		{name: "not nsc", url: "s3://my-bucket", wantErr: true},
		{name: "nsc without namespace", url: "nsc://", wantErr: true},
		{name: "invalid url", url: "nsc://host:notaport", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nsc, err := parseNscNamespace(tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseNscNamespace(%q) err = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
			if nsc != tt.nsc {
				t.Errorf("parseNscNamespace(%q) namespace = %q, want %q", tt.url, nsc, tt.nsc)
			}
		})
	}
}

func newTestNscStore(t *testing.T, api nscAPIClient) *NscStore {
	t.Helper()
	holder := newNscClient(func(context.Context) (nscAPIClient, error) {
		return api, nil
	})
	store, err := NewNscStore("nsc://test-namespace", holder)
	if err != nil {
		t.Fatalf("NewNscStore: %v", err)
	}
	return store
}

func TestNscStore_Upload(t *testing.T) {
	content := []byte("cache content")
	filePath := filepath.Join(t.TempDir(), "cache archive")
	if err := os.WriteFile(filePath, content, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var gotNsc, gotPath string
	var gotContent []byte
	var gotOpts storage.UploadOpts
	api := &fakeNscAPIClient{upload: func(_ context.Context, nsc, path string, r io.Reader, opts storage.UploadOpts) error {
		gotNsc = nsc
		gotPath = path
		gotOpts = opts
		var err error
		gotContent, err = io.ReadAll(r)
		return err
	}}
	store := newTestNscStore(t, api)

	before := time.Now()
	info, err := store.Upload(t.Context(), filePath, "artifact-key")
	after := time.Now()
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if gotNsc != "test-namespace" {
		t.Errorf("namespace = %q, want test-namespace", gotNsc)
	}
	if gotPath != "artifact-key" {
		t.Errorf("path = %q, want artifact-key", gotPath)
	}
	if !bytes.Equal(gotContent, content) {
		t.Errorf("content = %q, want %q", gotContent, content)
	}
	if gotOpts.Length != int64(len(content)) {
		t.Errorf("Length = %d, want %d", gotOpts.Length, len(content))
	}
	if gotOpts.ExpiresAt == nil || gotOpts.ExpiresAt.Before(before.Add(nscDefaultExpiry)) || gotOpts.ExpiresAt.After(after.Add(nscDefaultExpiry)) {
		t.Errorf("ExpiresAt = %v, want between %v and %v", gotOpts.ExpiresAt, before.Add(nscDefaultExpiry), after.Add(nscDefaultExpiry))
	}
	if info.BytesTransferred != int64(len(content)) {
		t.Errorf("BytesTransferred = %d, want %d", info.BytesTransferred, len(content))
	}
}

func TestNscStore_UploadFailure(t *testing.T) {
	wantErr := errors.New("upload failed")
	api := &fakeNscAPIClient{upload: func(context.Context, string, string, io.Reader, storage.UploadOpts) error {
		return wantErr
	}}
	store := newTestNscStore(t, api)
	filePath := filepath.Join(t.TempDir(), "cache")
	if err := os.WriteFile(filePath, []byte("data"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := store.Upload(t.Context(), filePath, "key"); !errors.Is(err, wantErr) {
		t.Fatalf("Upload error = %v, want %v", err, wantErr)
	}
}

func TestNscStore_UploadOpensFileBeforeInitializingClient(t *testing.T) {
	var initializeCalls atomic.Int32
	holder := newNscClient(func(context.Context) (nscAPIClient, error) {
		initializeCalls.Add(1)
		return &fakeNscAPIClient{}, nil
	})
	store, err := NewNscStore("nsc://ns", holder)
	if err != nil {
		t.Fatalf("NewNscStore: %v", err)
	}

	if _, err := store.Upload(t.Context(), filepath.Join(t.TempDir(), "missing"), "key"); err == nil {
		t.Fatal("Upload: expected error")
	}
	if got := initializeCalls.Load(); got != 0 {
		t.Errorf("initialization calls = %d, want 0", got)
	}
}

type trackingReadCloser struct {
	io.Reader
	closed   bool
	closeErr error
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return r.closeErr
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

type partialErrorReader struct {
	sent bool
}

func (r *partialErrorReader) Read(p []byte) (int, error) {
	if r.sent {
		return 0, io.ErrUnexpectedEOF
	}
	r.sent = true
	return copy(p, "partial"), nil
}

func TestNscStore_Download(t *testing.T) {
	body := &trackingReadCloser{Reader: strings.NewReader("downloaded content")}
	var gotNsc, gotPath string
	var extendReq *storagev1beta.ExtendArtifactRequest
	api := &fakeNscAPIClient{
		resolve: func(_ context.Context, nsc, path string) (io.ReadCloser, error) {
			gotNsc = nsc
			gotPath = path
			return body, nil
		},
		extend: func(_ context.Context, req *storagev1beta.ExtendArtifactRequest) error {
			extendReq = req
			return nil
		},
	}
	store := newTestNscStore(t, api)
	dest := filepath.Join(t.TempDir(), "downloaded")

	info, err := store.Download(t.Context(), "artifact-key", dest)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if gotNsc != "test-namespace" || gotPath != "artifact-key" {
		t.Errorf("ResolveArtifactStream(%q, %q), want (%q, %q)", gotNsc, gotPath, "test-namespace", "artifact-key")
	}
	if !body.closed {
		t.Error("download body was not closed")
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "downloaded content" {
		t.Errorf("downloaded content = %q", got)
	}
	if info.BytesTransferred != int64(len(got)) {
		t.Errorf("BytesTransferred = %d, want %d", info.BytesTransferred, len(got))
	}
	if extendReq == nil {
		t.Fatal("ExtendArtifact was not called")
	}
	if extendReq.GetNamespace() != "test-namespace" || extendReq.GetPath() != "artifact-key" {
		t.Errorf("ExtendArtifact request namespace/path = %q/%q", extendReq.GetNamespace(), extendReq.GetPath())
	}
	if got := extendReq.GetEnsureMinimum().AsDuration(); got != nscDefaultExpiry {
		t.Errorf("EnsureMinimum = %v, want %v", got, nscDefaultExpiry)
	}
}

func TestNscStore_DownloadNotFound(t *testing.T) {
	resolveCalls := 0
	api := &fakeNscAPIClient{resolve: func(context.Context, string, string) (io.ReadCloser, error) {
		resolveCalls++
		return nil, status.Error(codes.NotFound, "artifact expired")
	}}
	store := newTestNscStore(t, api)

	_, err := store.Download(t.Context(), "missing", filepath.Join(t.TempDir(), "dest"))
	if !errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("Download error = %v, want ErrBlobNotFound", err)
	}
	if resolveCalls != 1 {
		t.Errorf("resolve calls = %d, want 1", resolveCalls)
	}
}

func TestNscStore_DownloadStreamNotFound(t *testing.T) {
	resolveCalls := 0
	body := &trackingReadCloser{Reader: errorReader{err: status.Error(codes.NotFound, "artifact expired")}}
	api := &fakeNscAPIClient{resolve: func(context.Context, string, string) (io.ReadCloser, error) {
		resolveCalls++
		return body, nil
	}}
	store := newTestNscStore(t, api)

	_, err := store.Download(t.Context(), "missing", filepath.Join(t.TempDir(), "dest"))
	if !errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("Download error = %v, want ErrBlobNotFound", err)
	}
	if resolveCalls != 1 {
		t.Errorf("resolve calls = %d, want 1", resolveCalls)
	}
	if !body.closed {
		t.Error("download body was not closed")
	}
}

func TestNscStore_DownloadResolveFailure(t *testing.T) {
	wantErr := status.Error(codes.PermissionDenied, "permission denied")
	api := &fakeNscAPIClient{resolve: func(context.Context, string, string) (io.ReadCloser, error) {
		return nil, wantErr
	}}
	store := newTestNscStore(t, api)

	_, err := store.Download(t.Context(), "key", filepath.Join(t.TempDir(), "dest"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Download error = %v, want %v", err, wantErr)
	}
	if errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("Download error = %v, should not be ErrBlobNotFound", err)
	}
}

func TestNscStore_DownloadRetriesTransientResolveFailure(t *testing.T) {
	resolveCalls := 0
	api := &fakeNscAPIClient{resolve: func(context.Context, string, string) (io.ReadCloser, error) {
		resolveCalls++
		if resolveCalls == 1 {
			return nil, status.Error(codes.Unavailable, "unavailable")
		}
		return io.NopCloser(strings.NewReader("complete")), nil
	}}
	store := newTestNscStore(t, api)
	dest := filepath.Join(t.TempDir(), "dest")

	if _, err := store.Download(t.Context(), "key", dest); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if resolveCalls != 2 {
		t.Errorf("resolve calls = %d, want 2", resolveCalls)
	}
}

func TestNscStore_DownloadClosesBodyWhenDestinationCreationFails(t *testing.T) {
	body := &trackingReadCloser{Reader: strings.NewReader("data")}
	api := &fakeNscAPIClient{resolve: func(context.Context, string, string) (io.ReadCloser, error) {
		return body, nil
	}}
	store := newTestNscStore(t, api)

	if _, err := store.Download(t.Context(), "key", t.TempDir()); err == nil {
		t.Fatal("Download: expected error")
	}
	if !body.closed {
		t.Error("download body was not closed")
	}
}

func TestNscStore_DownloadClosesBodyWhenCopyFails(t *testing.T) {
	wantErr := errors.New("read failed")
	body := &trackingReadCloser{Reader: errorReader{err: wantErr}}
	api := &fakeNscAPIClient{resolve: func(context.Context, string, string) (io.ReadCloser, error) {
		return body, nil
	}}
	store := newTestNscStore(t, api)

	_, err := store.Download(t.Context(), "key", filepath.Join(t.TempDir(), "dest"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Download error = %v, want %v", err, wantErr)
	}
	if !body.closed {
		t.Error("download body was not closed")
	}
}

func TestNscStore_DownloadRetriesCopyFailureAndTruncatesDestination(t *testing.T) {
	firstBody := &trackingReadCloser{Reader: &partialErrorReader{}}
	secondBody := &trackingReadCloser{Reader: strings.NewReader("complete")}
	resolveCalls := 0
	api := &fakeNscAPIClient{resolve: func(context.Context, string, string) (io.ReadCloser, error) {
		resolveCalls++
		if resolveCalls == 1 {
			return firstBody, nil
		}
		return secondBody, nil
	}}
	store := newTestNscStore(t, api)
	dest := filepath.Join(t.TempDir(), "dest")

	if _, err := store.Download(t.Context(), "key", dest); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if resolveCalls != 2 {
		t.Errorf("resolve calls = %d, want 2", resolveCalls)
	}
	if !firstBody.closed || !secondBody.closed {
		t.Errorf("body closure = first:%t second:%t, want both closed", firstBody.closed, secondBody.closed)
	}
	content, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(content); got != "complete" {
		t.Errorf("downloaded content = %q, want complete", got)
	}
}

func TestIsRetryableNscDownloadError_HTTP2StreamError(t *testing.T) {
	err := fmt.Errorf("read response body: %w", errors.New("stream error: stream ID 1; CANCEL; received from peer"))
	if !isRetryableNscDownloadError(err) {
		t.Errorf("isRetryableNscDownloadError(%v) = false, want true", err)
	}
}

func TestNscStore_DownloadFailsWhenBodyCloseFails(t *testing.T) {
	wantErr := errors.New("close failed")
	body := &trackingReadCloser{Reader: strings.NewReader("data"), closeErr: wantErr}
	api := &fakeNscAPIClient{resolve: func(context.Context, string, string) (io.ReadCloser, error) {
		return body, nil
	}}
	store := newTestNscStore(t, api)

	_, err := store.Download(t.Context(), "key", filepath.Join(t.TempDir(), "dest"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Download error = %v, want %v", err, wantErr)
	}
}

func TestNscStore_DownloadSucceedsWhenRefreshFails(t *testing.T) {
	wantErr := errors.New("extend failed")
	api := &fakeNscAPIClient{
		resolve: func(context.Context, string, string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("data")), nil
		},
		extend: func(context.Context, *storagev1beta.ExtendArtifactRequest) error {
			return wantErr
		},
	}
	store := newTestNscStore(t, api)

	if _, err := store.Download(t.Context(), "key", filepath.Join(t.TempDir(), "dest")); err != nil {
		t.Fatalf("Download should succeed despite failed expiry refresh: %v", err)
	}
}

func TestNewNscStore_RequiresNamespaceAndClient(t *testing.T) {
	holder := newNscClient(func(context.Context) (nscAPIClient, error) {
		return &fakeNscAPIClient{}, nil
	})
	if _, err := NewNscStore("nsc://", holder); err == nil {
		t.Error(`NewNscStore("nsc://"): expected error`)
	}
	if _, err := NewNscStore("nsc://namespace", nil); err == nil {
		t.Error("NewNscStore with nil client: expected error")
	}
}

func TestNewBlobStore_NscScheme(t *testing.T) {
	holder := newNscClient(func(context.Context) (nscAPIClient, error) {
		return &fakeNscAPIClient{}, nil
	})
	blob, err := NewBlobStore(t.Context(), AgentManaged, "nsc://my-namespace", holder)
	if err != nil {
		t.Fatalf("NewBlobStore: %v", err)
	}
	nsc, ok := blob.(*NscStore)
	if !ok {
		t.Fatalf("NewBlobStore returned %T, want *NscStore", blob)
	}
	if nsc.nsc != "my-namespace" {
		t.Errorf("namespace = %q, want my-namespace", nsc.nsc)
	}

	if _, err := NewBlobStore(t.Context(), AgentManaged, "nsc://my-namespace", nil); err == nil {
		t.Error("NewBlobStore with nil Namespace client: expected error")
	}
}

func TestNscStore_Integration(t *testing.T) {
	if os.Getenv("NSC_INTEGRATION_TEST") == "" {
		t.Skip("Skipping NSC integration test (set NSC_INTEGRATION_TEST=1 to run)")
	}

	holder := NewNscClient()
	t.Cleanup(func() {
		if err := holder.Close(); err != nil {
			t.Errorf("close Namespace client: %v", err)
		}
	})
	store, err := NewNscStore("nsc://main", holder)
	if err != nil {
		t.Fatalf("NewNscStore: %v", err)
	}

	testFile := filepath.Join(t.TempDir(), "test-upload.txt")
	testContent := "Hello from NSC integration test!"
	if err := os.WriteFile(testFile, []byte(testContent), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	key := "integration-test/test-file.txt"
	if _, err := store.Upload(t.Context(), testFile, key); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	downloadFile := filepath.Join(t.TempDir(), "test-download.txt")
	if _, err := store.Download(t.Context(), key, downloadFile); err != nil {
		t.Fatalf("Download: %v", err)
	}
	downloadedContent, err := os.ReadFile(downloadFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(downloadedContent) != testContent {
		t.Errorf("downloaded content = %q, want %q", downloadedContent, testContent)
	}
}
