package store

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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"namespacelabs.dev/integrations/api/storage"
	storagev1beta "namespacelabs.dev/integrations/proto/namespace/cloud/storage/v1beta"
)

func TestNscStore_Interface(t *testing.T) {
	var _ Blob = (*NscStore)(nil)
	var _ RetentionRefresher = (*NscStore)(nil)
}

type fakeNscAPIClient struct {
	upload     func(context.Context, string, string, io.Reader, storage.UploadOpts) error
	download   func(context.Context, string, string, string) error
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

func (c *fakeNscAPIClient) DownloadArtifact(ctx context.Context, nsc, path, destPath string) error {
	if c.download == nil {
		return os.WriteFile(destPath, nil, 0o600)
	}
	return c.download(ctx, nsc, path, destPath)
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
	var calls atomic.Int32
	client := newNscClient(func(context.Context) (nscAPIClient, error) {
		calls.Add(1)
		return &fakeNscAPIClient{}, nil
	})
	if got := calls.Load(); got != 0 {
		t.Fatalf("initialization calls after construction = %d, want 0", got)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("initialization calls after closing unused client = %d, want 0", got)
	}
}

func TestNscClient_ConcurrentInitialization(t *testing.T) {
	var calls atomic.Int32
	want := &fakeNscAPIClient{}
	client := newNscClient(func(context.Context) (nscAPIClient, error) {
		calls.Add(1)
		return want, nil
	})
	const callers = 32
	results := make(chan nscAPIClient, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := client.get(t.Context())
			if err != nil {
				t.Errorf("get: %v", err)
			}
			results <- got
		}()
	}
	wg.Wait()
	close(results)
	for got := range results {
		if got != want {
			t.Errorf("get returned %p, want %p", got, want)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("initialization calls = %d, want 1", got)
	}
}

func TestNscClient_InitializationError(t *testing.T) {
	wantErr := errors.New("initialization failed")
	var calls atomic.Int32
	client := newNscClient(func(context.Context) (nscAPIClient, error) {
		calls.Add(1)
		return nil, wantErr
	})
	for range 2 {
		if got, err := client.get(t.Context()); got != nil || !errors.Is(err, wantErr) {
			t.Errorf("get = (%v, %v), want (nil, %v)", got, err, wantErr)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("initialization calls = %d, want 1", got)
	}
}

func TestNscClient_CloseOnce(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{{name: "success"}, {name: "error", err: errors.New("close failed")}} {
		t.Run(test.name, func(t *testing.T) {
			api := &fakeNscAPIClient{close: func() error { return test.err }}
			client := newNscClient(func(context.Context) (nscAPIClient, error) { return api, nil })
			if _, err := client.get(t.Context()); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := client.Close(); !errors.Is(err, test.err) {
					t.Errorf("Close = %v, want %v", err, test.err)
				}
			}
			if got := api.closeCalls.Load(); got != 1 {
				t.Errorf("close calls = %d, want 1", got)
			}
		})
	}
}

func TestParseNscNamespace(t *testing.T) {
	tests := []struct{ url, nsc string }{{"nsc://my-namespace", "my-namespace"}, {"s3://bucket", ""}, {"nsc://", ""}, {"nsc://host:notaport", ""}}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got, err := parseNscNamespace(tt.url)
			if (err == nil) != (tt.nsc != "") || got != tt.nsc {
				t.Errorf("parseNscNamespace(%q) = (%q, %v), want %q", tt.url, got, err, tt.nsc)
			}
		})
	}
}

func newTestNscStore(t *testing.T, api nscAPIClient) *NscStore {
	t.Helper()
	holder := newNscClient(func(context.Context) (nscAPIClient, error) { return api, nil })
	store, err := NewNscStore("nsc://test-namespace", holder)
	if err != nil {
		t.Fatalf("NewNscStore: %v", err)
	}
	return store
}

func TestNscStore_UploadRetention(t *testing.T) {
	tests := []struct {
		name      string
		retention time.Duration
		want      time.Duration
	}{
		{name: "supplied", retention: 48 * time.Hour, want: 48 * time.Hour},
		{name: "default zero", want: 72 * time.Hour},
		{name: "default negative", retention: -time.Hour, want: 72 * time.Hour},
		{name: "rounded up", retention: 25*time.Hour + time.Second, want: 26 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := []byte("cache content")
			filePath := filepath.Join(t.TempDir(), "cache archive")
			if err := os.WriteFile(filePath, content, 0o600); err != nil {
				t.Fatal(err)
			}
			var gotNsc, gotPath string
			var gotContent []byte
			var gotOpts storage.UploadOpts
			api := &fakeNscAPIClient{upload: func(_ context.Context, nsc, path string, r io.Reader, opts storage.UploadOpts) error {
				gotNsc, gotPath, gotOpts = nsc, path, opts
				gotContent, _ = io.ReadAll(r)
				return nil
			}}
			before := time.Now()
			info, err := newTestNscStore(t, api).Upload(t.Context(), filePath, "artifact-key", tt.retention)
			after := time.Now()
			if err != nil {
				t.Fatalf("Upload: %v", err)
			}
			if gotNsc != "test-namespace" || gotPath != "artifact-key" || !bytes.Equal(gotContent, content) {
				t.Errorf("upload = namespace %q, path %q, content %q", gotNsc, gotPath, gotContent)
			}
			if gotOpts.Length != int64(len(content)) {
				t.Errorf("Length = %d, want %d", gotOpts.Length, len(content))
			}
			if gotOpts.ExpiresAt == nil || gotOpts.ExpiresAt.Before(before.Add(tt.want)) || gotOpts.ExpiresAt.After(after.Add(tt.want)) {
				t.Errorf("ExpiresAt = %v, want now + %v", gotOpts.ExpiresAt, tt.want)
			}
			if info.BytesTransferred != int64(len(content)) {
				t.Errorf("BytesTransferred = %d, want %d", info.BytesTransferred, len(content))
			}
		})
	}
}

func TestNscStore_UploadFailureAndFileOrdering(t *testing.T) {
	wantErr := errors.New("upload failed")
	store := newTestNscStore(t, &fakeNscAPIClient{upload: func(context.Context, string, string, io.Reader, storage.UploadOpts) error { return wantErr }})
	filePath := filepath.Join(t.TempDir(), "cache")
	if err := os.WriteFile(filePath, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upload(t.Context(), filePath, "key", time.Hour); !errors.Is(err, wantErr) {
		t.Fatalf("Upload error = %v, want %v", err, wantErr)
	}

	var calls atomic.Int32
	holder := newNscClient(func(context.Context) (nscAPIClient, error) { calls.Add(1); return &fakeNscAPIClient{}, nil })
	store, _ = NewNscStore("nsc://ns", holder)
	if _, err := store.Upload(t.Context(), filepath.Join(t.TempDir(), "missing"), "key", time.Hour); err == nil {
		t.Fatal("Upload: expected error")
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("initialization calls = %d, want 0", got)
	}
}

func TestNscStore_DownloadDoesNotRefresh(t *testing.T) {
	var gotNsc, gotPath, gotDest string
	var extendCalls atomic.Int32
	api := &fakeNscAPIClient{
		download: func(_ context.Context, nsc, path, dest string) error {
			gotNsc, gotPath, gotDest = nsc, path, dest
			return os.WriteFile(dest, []byte("downloaded content"), 0o600)
		},
		extend: func(context.Context, *storagev1beta.ExtendArtifactRequest) error { extendCalls.Add(1); return nil },
	}
	dest := filepath.Join(t.TempDir(), "downloaded")
	info, err := newTestNscStore(t, api).Download(t.Context(), "artifact-key", dest)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if gotNsc != "test-namespace" || gotPath != "artifact-key" || gotDest != dest {
		t.Errorf("DownloadArtifact(%q, %q, %q)", gotNsc, gotPath, gotDest)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "downloaded content" || info.BytesTransferred != int64(len(got)) {
		t.Errorf("download = %q, bytes = %d", got, info.BytesTransferred)
	}
	if got := extendCalls.Load(); got != 0 {
		t.Errorf("ExtendArtifact calls during Download = %d, want 0", got)
	}
}

func TestNscStore_DownloadNotFound(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "grpc", err: status.Error(codes.NotFound, "artifact expired")},
		{name: "SDK HTTP", err: errors.New("inspect download: unexpected HTTP status 404")},
	} {
		t.Run(test.name, func(t *testing.T) {
			api := &fakeNscAPIClient{download: func(context.Context, string, string, string) error { return test.err }}
			_, err := newTestNscStore(t, api).Download(t.Context(), "missing", filepath.Join(t.TempDir(), "dest"))
			if !errors.Is(err, ErrBlobNotFound) {
				t.Fatalf("Download error = %v, want ErrBlobNotFound", err)
			}
		})
	}
}

func TestNscStore_RefreshRetention(t *testing.T) {
	tests := []struct {
		name      string
		retention time.Duration
		want      time.Duration
	}{{"supplied", 48 * time.Hour, 48 * time.Hour}, {"default", 0, nscDefaultRetention}, {"rounded", time.Hour + time.Nanosecond, 2 * time.Hour}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got *storagev1beta.ExtendArtifactRequest
			api := &fakeNscAPIClient{extend: func(_ context.Context, req *storagev1beta.ExtendArtifactRequest) error {
				got = req
				return errors.New("best effort")
			}}
			newTestNscStore(t, api).RefreshRetention(t.Context(), "key", tt.retention)
			if got == nil || got.GetNamespace() != "test-namespace" || got.GetPath() != "key" || got.GetEnsureMinimum().AsDuration() != tt.want {
				t.Errorf("ExtendArtifact request = %v, want namespace/path/retention test-namespace/key/%v", got, tt.want)
			}
		})
	}
}

type fakeArtifactsClient struct {
	storagev1beta.ArtifactsServiceClient
	resolve func(context.Context, *storagev1beta.ResolveArtifactRequest) (*storagev1beta.ResolveArtifactResponse, error)
}

func (c *fakeArtifactsClient) ResolveArtifact(ctx context.Context, req *storagev1beta.ResolveArtifactRequest, _ ...grpc.CallOption) (*storagev1beta.ResolveArtifactResponse, error) {
	return c.resolve(ctx, req)
}

func testStorageClient(t *testing.T, resolve func(context.Context, *storagev1beta.ResolveArtifactRequest) (*storagev1beta.ResolveArtifactResponse, error)) *nscStorageClient {
	t.Helper()
	return &nscStorageClient{client: storage.Client{Artifacts: &fakeArtifactsClient{resolve: resolve}}}
}

func testData(size int) []byte {
	pattern := []byte("namespace-artifact-")
	return bytes.Repeat(pattern, (size+len(pattern)-1)/len(pattern))[:size]
}

func TestNscStorageClient_DownloadArtifactRanges(t *testing.T) {
	const chunk = 4 * 1024 * 1024
	data := testData(4*chunk + 17)
	var active, maxActive atomic.Int32
	ready := make(chan struct{})
	var mu sync.Mutex
	requests := map[int64]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Method == http.MethodHead {
			return
		}
		var start, end int64
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil {
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests[start]++
		mu.Unlock()
		n := active.Add(1)
		defer active.Add(-1)
		for old := maxActive.Load(); n > old && !maxActive.CompareAndSwap(old, n); old = maxActive.Load() {
		}
		if n == 4 {
			select {
			case <-ready:
			default:
				close(ready)
			}
		}
		select {
		case <-ready:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start : end+1])
	}))
	t.Cleanup(server.Close)
	var resolves atomic.Int32
	client := testStorageClient(t, func(_ context.Context, req *storagev1beta.ResolveArtifactRequest) (*storagev1beta.ResolveArtifactResponse, error) {
		if req.GetNamespace() != "main" || req.GetPath() != "key" {
			t.Errorf("resolve request = %s/%s", req.GetNamespace(), req.GetPath())
		}
		resolves.Add(1)
		return &storagev1beta.ResolveArtifactResponse{SignedDownloadUrl: server.URL}, nil
	})
	dest := filepath.Join(t.TempDir(), "artifact")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := client.DownloadArtifact(ctx, "main", "key", dest); err != nil {
		t.Fatalf("DownloadArtifact: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Error("downloaded content differs")
	}
	if got := maxActive.Load(); got != 4 {
		t.Errorf("maximum concurrent requests = %d, want 4", got)
	}
	if got := resolves.Load(); got != 6 {
		t.Errorf("ResolveArtifact calls = %d, want 6", got)
	}
	mu.Lock()
	defer mu.Unlock()
	for start := int64(0); start < int64(len(data)); start += chunk {
		if requests[start] != 1 {
			t.Errorf("requests at %d = %d, want 1", start, requests[start])
		}
	}
}

func TestNscStorageClient_DownloadArtifactRetriesOnlyFailedChunk(t *testing.T) {
	const chunk = 4 * 1024 * 1024
	data := testData(2*chunk + 1)
	var mu sync.Mutex
	requests := map[int64]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Method == http.MethodHead {
			return
		}
		var start, end int64
		_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end)
		mu.Lock()
		requests[start]++
		attempt := requests[start]
		mu.Unlock()
		if start == chunk && attempt == 1 {
			http.Error(w, "retry", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(data[start : end+1])
	}))
	t.Cleanup(server.Close)
	client := testStorageClient(t, func(context.Context, *storagev1beta.ResolveArtifactRequest) (*storagev1beta.ResolveArtifactResponse, error) {
		return &storagev1beta.ResolveArtifactResponse{SignedDownloadUrl: server.URL}, nil
	})
	dest := filepath.Join(t.TempDir(), "artifact")
	if err := client.DownloadArtifact(t.Context(), "main", "key", dest); err != nil {
		t.Fatalf("DownloadArtifact: %v", err)
	}
	if got, err := os.ReadFile(dest); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("downloaded content differs: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests[0] != 1 || requests[chunk] != 2 || requests[2*chunk] != 1 {
		t.Errorf("range request counts = %v", requests)
	}
}

func TestNscStorageClient_DownloadArtifactFallback(t *testing.T) {
	for _, test := range []struct {
		name          string
		size          int
		ranges, known bool
	}{
		{name: "small", size: 1024, ranges: true, known: true},
		{name: "nonrange", size: 5 * 1024 * 1024, known: true},
		{name: "unknown", size: 1024},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := testData(test.size)
			var gets atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.known {
					w.Header().Set("Content-Length", strconv.Itoa(len(data)))
				}
				if test.ranges {
					w.Header().Set("Accept-Ranges", "bytes")
				}
				if r.Method == http.MethodHead {
					return
				}
				gets.Add(1)
				if r.Header.Get("Range") != "" {
					t.Errorf("unexpected Range header %q", r.Header.Get("Range"))
				}
				_, _ = w.Write(data)
			}))
			t.Cleanup(server.Close)
			client := testStorageClient(t, func(context.Context, *storagev1beta.ResolveArtifactRequest) (*storagev1beta.ResolveArtifactResponse, error) {
				return &storagev1beta.ResolveArtifactResponse{SignedDownloadUrl: server.URL}, nil
			})
			dest := filepath.Join(t.TempDir(), "artifact")
			if err := client.DownloadArtifact(t.Context(), "main", "key", dest); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(dest)
			if !bytes.Equal(got, data) || gets.Load() != 1 {
				t.Errorf("content match = %t, GETs = %d", bytes.Equal(got, data), gets.Load())
			}
		})
	}
}

func TestNscStorageClient_DownloadArtifactCancellationCleansAndWaits(t *testing.T) {
	const chunk = 4 * 1024 * 1024
	data := testData(2 * chunk)
	started := make(chan struct{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Method == http.MethodHead {
			return
		}
		started <- struct{}{}
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	client := testStorageClient(t, func(context.Context, *storagev1beta.ResolveArtifactRequest) (*storagev1beta.ResolveArtifactResponse, error) {
		return &storagev1beta.ResolveArtifactResponse{SignedDownloadUrl: server.URL}, nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	dest := filepath.Join(t.TempDir(), "artifact")
	done := make(chan error, 1)
	go func() { done <- client.DownloadArtifact(ctx, "main", "key", dest) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("download did not start")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("DownloadArtifact error = %v, want context canceled", err)
	}
	if entries, err := os.ReadDir(filepath.Dir(dest)); err != nil || len(entries) != 0 {
		t.Errorf("download directory not empty after cancellation: %v, %v", entries, err)
	}
}

func TestNscStorageClient_DownloadArtifactMissing(t *testing.T) {
	t.Run("grpc", func(t *testing.T) {
		client := testStorageClient(t, func(context.Context, *storagev1beta.ResolveArtifactRequest) (*storagev1beta.ResolveArtifactResponse, error) {
			return nil, status.Error(codes.NotFound, "missing")
		})
		if err := client.DownloadArtifact(t.Context(), "main", "key", filepath.Join(t.TempDir(), "dest")); status.Code(err) != codes.NotFound {
			t.Fatalf("error = %v, want NotFound", err)
		}
	})
	t.Run("HTTP", func(t *testing.T) {
		server := httptest.NewServer(http.NotFoundHandler())
		t.Cleanup(server.Close)
		client := testStorageClient(t, func(context.Context, *storagev1beta.ResolveArtifactRequest) (*storagev1beta.ResolveArtifactResponse, error) {
			return &storagev1beta.ResolveArtifactResponse{SignedDownloadUrl: server.URL}, nil
		})
		err := client.DownloadArtifact(t.Context(), "main", "key", filepath.Join(t.TempDir(), "dest"))
		if err == nil || !strings.Contains(err.Error(), "unexpected HTTP status 404") {
			t.Fatalf("error = %v, want unexpected HTTP status 404", err)
		}
	})
}

func TestNewNscStoreAndBlobStore(t *testing.T) {
	holder := newNscClient(func(context.Context) (nscAPIClient, error) { return &fakeNscAPIClient{}, nil })
	if _, err := NewNscStore("nsc://", holder); err == nil {
		t.Error("missing namespace: expected error")
	}
	if _, err := NewNscStore("nsc://namespace", nil); err == nil {
		t.Error("nil client: expected error")
	}
	blob, err := NewBlobStore(t.Context(), AgentManaged, "nsc://my-namespace", holder)
	if err != nil {
		t.Fatalf("NewBlobStore: %v", err)
	}
	if nsc, ok := blob.(*NscStore); !ok || nsc.nsc != "my-namespace" {
		t.Errorf("NewBlobStore = %#v", blob)
	}
}

func TestNscStore_Integration(t *testing.T) {
	if os.Getenv("NSC_INTEGRATION_TEST") == "" {
		t.Skip("set NSC_INTEGRATION_TEST=1 to run")
	}
	holder := NewNscClient()
	t.Cleanup(func() {
		if err := holder.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	store, err := NewNscStore("nsc://main", holder)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("Hello from NSC integration test!")
	source := filepath.Join(t.TempDir(), "upload")
	if err := os.WriteFile(source, want, 0o600); err != nil {
		t.Fatal(err)
	}
	key := "integration-test/test-file.txt"
	if _, err := store.Upload(t.Context(), source, key, nscDefaultRetention); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "download")
	if _, err := store.Download(t.Context(), key, dest); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(dest); err != nil || !bytes.Equal(got, want) {
		t.Errorf("download = %q, %v", got, err)
	}
}
