package store

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/buildkite/agent/v4/internal/cache/internal/trace"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"namespacelabs.dev/integrations/api/storage"
	"namespacelabs.dev/integrations/auth"
	storagev1beta "namespacelabs.dev/integrations/proto/namespace/cloud/storage/v1beta"
	"namespacelabs.dev/integrations/storage/downloader"
)

// nscScheme is the URL scheme that routes an agent-managed cache store to NSC.
const nscScheme = "nsc"

const nscDefaultRetention = 72 * time.Hour

// nscRetention preserves the CLI's whole-hour rounding and older-server fallback.
func nscRetention(retention time.Duration) time.Duration {
	if retention <= 0 {
		return nscDefaultRetention
	}
	return ((retention + time.Hour - 1) / time.Hour) * time.Hour
}

type nscAPIClient interface {
	UploadArtifact(context.Context, string, string, io.Reader, storage.UploadOpts) error
	DownloadArtifact(context.Context, string, string, string) error
	ExtendArtifact(context.Context, *storagev1beta.ExtendArtifactRequest) error
	Close() error
}

// Namespace Storage API documentation:
// https://buf.build/namespace/cloud/docs/main:namespace.cloud.storage.v1beta
type nscStorageClient struct {
	client storage.Client
}

func (c *nscStorageClient) UploadArtifact(ctx context.Context, nsc, path string, r io.Reader, opts storage.UploadOpts) error {
	_, err := storage.UploadArtifactWithOpts(ctx, c.client, nsc, path, r, opts)
	return err
}

func (c *nscStorageClient) DownloadArtifact(ctx context.Context, nsc, path, destPath string) error {
	return downloader.DownloadArtifact(ctx, c.client, nsc, path, destPath, downloader.Options{})
}

func (c *nscStorageClient) ExtendArtifact(ctx context.Context, req *storagev1beta.ExtendArtifactRequest) error {
	_, err := c.client.Artifacts.ExtendArtifact(ctx, req)
	return err
}

func (c *nscStorageClient) Close() error {
	return c.client.Close()
}

type nscClientFactory func(context.Context) (nscAPIClient, error)

// NscClient lazily initializes one Namespace Storage API client for use by all
// NSC cache transfers in a cache command.
type NscClient struct {
	initialize nscClientFactory
	initOnce   sync.Once
	client     nscAPIClient
	initErr    error
	closeOnce  sync.Once
	closeErr   error
}

// NewNscClient creates a lazy Namespace Storage API client holder.
func NewNscClient() *NscClient {
	return newNscClient(newNscAPIClient)
}

func newNscClient(initialize nscClientFactory) *NscClient {
	return &NscClient{initialize: initialize}
}

func newNscAPIClient(ctx context.Context) (nscAPIClient, error) {
	token, err := auth.LoadDefaults()
	if err != nil {
		return nil, fmt.Errorf("load Namespace authentication: %w", err)
	}

	client, err := storage.NewClient(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("create Namespace Storage API client: %w", err)
	}
	return &nscStorageClient{client: client}, nil
}

func (c *NscClient) get(ctx context.Context) (nscAPIClient, error) {
	c.initOnce.Do(func() {
		c.client, c.initErr = c.initialize(ctx)
	})
	return c.client, c.initErr
}

// Close closes the initialized Namespace Storage API client. It is a no-op if
// no NSC cache transfer initialized the client.
func (c *NscClient) Close() error {
	if c.client == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		c.closeErr = c.client.Close()
	})
	return c.closeErr
}

// NscStore implements Blob using the Namespace Storage API.
type NscStore struct {
	nsc    string
	client *NscClient
}

// NewNscStore creates a Namespace Storage API-backed store.
func NewNscStore(bucketURL string, client *NscClient) (*NscStore, error) {
	if client == nil {
		return nil, fmt.Errorf("namespace client is required for nsc:// cache stores")
	}
	nsc, err := parseNscNamespace(bucketURL)
	if err != nil {
		return nil, err
	}
	return &NscStore{nsc: nsc, client: client}, nil
}

// parseNscNamespace extracts the namespace from an nsc://<namespace> cache store
// URL. It errors on a non-nsc URL or a missing namespace.
func parseNscNamespace(bucketURL string) (string, error) {
	u, err := url.Parse(bucketURL)
	if err != nil {
		return "", fmt.Errorf("invalid cache store URL %q: %w", bucketURL, err)
	}
	if u.Scheme != nscScheme {
		return "", fmt.Errorf("expected %s:// cache store URL, got %q", nscScheme, bucketURL)
	}
	if u.Host == "" {
		return "", fmt.Errorf("nsc:// URL must include a namespace, e.g. nsc://my-namespace")
	}
	return u.Host, nil
}

func (n *NscStore) Upload(ctx context.Context, filePath, key string, retention time.Duration) (*TransferInfo, error) {
	_, span := trace.Start(ctx, "NscStore.Upload")
	defer span.End()

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open upload file: %w", err)
	}
	defer func() { _ = file.Close() }()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("get upload file info: %w", err)
	}

	client, err := n.client.get(ctx)
	if err != nil {
		return nil, fmt.Errorf("initialize Namespace client for upload: %w", err)
	}

	start := time.Now()
	expiresAt := start.Add(nscRetention(retention))
	if err := client.UploadArtifact(ctx, n.nsc, key, file, storage.UploadOpts{
		ExpiresAt: &expiresAt,
		Length:    fileInfo.Size(),
	}); err != nil {
		return nil, fmt.Errorf("upload Namespace artifact %q: %w", key, err)
	}

	duration := time.Since(start)
	bytesTransferred := fileInfo.Size()
	averageSpeed := calculateTransferSpeedMBps(bytesTransferred, duration)

	span.SetAttributes(
		attribute.Int64("bytes_transferred", bytesTransferred),
		attribute.String("transfer_speed", fmt.Sprintf("%.2fMB/s", averageSpeed)),
		attribute.String("nsc_key", key),
	)

	return &TransferInfo{
		BytesTransferred: bytesTransferred,
		TransferSpeed:    averageSpeed,
		RequestID:        "", // The Namespace Storage API does not expose request IDs.
		Duration:         duration,
	}, nil
}

func (n *NscStore) Download(ctx context.Context, key, filePath string) (*TransferInfo, error) {
	_, span := trace.Start(ctx, "NscStore.Download")
	defer span.End()

	client, err := n.client.get(ctx)
	if err != nil {
		return nil, fmt.Errorf("initialize Namespace client for download: %w", err)
	}

	start := time.Now()
	err = client.DownloadArtifact(ctx, n.nsc, key, filePath)
	// The SDK preserves gRPC errors but exposes HTTP statuses only as text.
	if status.Code(err) == codes.NotFound || err != nil && strings.HasSuffix(err.Error(), ": unexpected HTTP status 404") {
		return nil, fmt.Errorf("%w: nsc key %s: %w", ErrBlobNotFound, key, err)
	}
	if err != nil {
		return nil, fmt.Errorf("download Namespace artifact %q: %w", key, err)
	}

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("get downloaded file info: %w", err)
	}
	bytesTransferred := fileInfo.Size()
	duration := time.Since(start)
	averageSpeed := calculateTransferSpeedMBps(bytesTransferred, duration)

	span.SetAttributes(
		attribute.Int64("bytes_transferred", bytesTransferred),
		attribute.String("transfer_speed", fmt.Sprintf("%.2fMB/s", averageSpeed)),
		attribute.String("nsc_key", key),
	)

	return &TransferInfo{
		BytesTransferred: bytesTransferred,
		TransferSpeed:    averageSpeed,
		RequestID:        "", // The Namespace Storage API does not expose request IDs.
		Duration:         duration,
	}, nil
}

// RefreshRetention is best-effort. The restore flow decides when to refresh,
// including skipping fallback matches; Download must not refresh by itself.
func (n *NscStore) RefreshRetention(ctx context.Context, key string, retention time.Duration) {
	client, err := n.client.get(ctx)
	if err != nil {
		slog.Warn("failed to initialize cache TTL refresh, continuing (non-fatal)", "key", key, "error", err)
		return
	}
	err = client.ExtendArtifact(ctx, &storagev1beta.ExtendArtifactRequest{
		Path:          key,
		Namespace:     n.nsc,
		EnsureMinimum: durationpb.New(nscRetention(retention)),
	})
	if err != nil {
		slog.Warn("failed to refresh cache TTL, continuing (non-fatal)", "key", key, "error", err)
		return
	}
	slog.Debug("refreshed cache TTL", "key", key)
}
