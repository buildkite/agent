package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	storagev1beta "buf.build/gen/go/namespace/cloud/protocolbuffers/go/proto/namespace/cloud/storage/v1beta"
	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/cache/internal/trace"
	"github.com/buildkite/roko"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"namespacelabs.dev/integrations/api/storage"
	"namespacelabs.dev/integrations/auth"
)

// nscScheme is the URL scheme that routes an agent-managed cache store to NSC.
const nscScheme = "nsc"

const (
	// nscDefaultExpiry is the artifact lifetime used both when uploading a cache
	// entry and when refreshing it on access.
	nscDefaultExpiry = 24 * time.Hour

	// Match the retry allowance of the NSC CLI downloader this implementation replaces.
	nscDownloadAttempts = 10
)

type nscAPIClient interface {
	UploadArtifact(context.Context, string, string, io.Reader, storage.UploadOpts) error
	ResolveArtifactStream(context.Context, string, string) (io.ReadCloser, error)
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

func (c *nscStorageClient) ResolveArtifactStream(ctx context.Context, nsc, path string) (io.ReadCloser, error) {
	return storage.ResolveArtifactStream(ctx, c.client, nsc, path)
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

func (n *NscStore) Upload(ctx context.Context, filePath, key string) (*TransferInfo, error) {
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
	expiresAt := start.Add(nscDefaultExpiry)
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
	var bytesTransferred int64
	err = roko.NewRetrier(
		roko.WithMaxAttempts(nscDownloadAttempts),
		roko.WithStrategy(roko.ExponentialSubsecond(200*time.Millisecond)),
		roko.WithJitterRange(0, 250*time.Millisecond),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		var err error
		bytesTransferred, err = n.downloadOnce(ctx, client, key, filePath)
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrBlobNotFound) || !isRetryableNscDownloadError(err) {
			r.Break()
			return err
		}
		slog.Warn("NSC cache download failed, retrying", "key", key, "error", err, "retrier", r.String())
		return err
	})
	if err != nil {
		return nil, err
	}

	duration := time.Since(start)
	averageSpeed := calculateTransferSpeedMBps(bytesTransferred, duration)

	span.SetAttributes(
		attribute.Int64("bytes_transferred", bytesTransferred),
		attribute.String("transfer_speed", fmt.Sprintf("%.2fMB/s", averageSpeed)),
		attribute.String("nsc_key", key),
	)

	n.refreshExpiry(ctx, client, key)

	return &TransferInfo{
		BytesTransferred: bytesTransferred,
		TransferSpeed:    averageSpeed,
		RequestID:        "", // The Namespace Storage API does not expose request IDs.
		Duration:         duration,
	}, nil
}

func (n *NscStore) downloadOnce(ctx context.Context, client nscAPIClient, key, filePath string) (int64, error) {
	body, err := client.ResolveArtifactStream(ctx, n.nsc, key)
	if status.Code(err) == codes.NotFound {
		return 0, fmt.Errorf("%w: nsc key %s: %w", ErrBlobNotFound, key, err)
	}
	if err != nil {
		return 0, fmt.Errorf("resolve Namespace artifact %q: %w", key, err)
	}

	dest, err := os.Create(filePath)
	if err != nil {
		_ = body.Close()
		return 0, fmt.Errorf("create download file: %w", err)
	}

	bytesTransferred, copyErr := io.Copy(dest, body)
	destCloseErr := dest.Close()
	bodyCloseErr := body.Close()
	switch {
	case copyErr != nil:
		return 0, fmt.Errorf("download Namespace artifact %q: %w", key, copyErr)
	case destCloseErr != nil:
		return 0, fmt.Errorf("close download file: %w", destCloseErr)
	case bodyCloseErr != nil:
		return 0, fmt.Errorf("close Namespace artifact stream %q: %w", key, bodyCloseErr)
	default:
		return bytesTransferred, nil
	}
}

func isRetryableNscDownloadError(err error) bool {
	// net/http represents HTTP/2 stream resets with an unexported error type,
	// so its stable Error prefix is the only available classification seam.
	if strings.Contains(err.Error(), "stream error: stream ID ") {
		return true
	}
	switch status.Code(err) {
	case codes.Aborted, codes.DeadlineExceeded, codes.Internal, codes.ResourceExhausted, codes.Unavailable:
		return true
	default:
		return api.IsRetryableError(err)
	}
}

// refreshExpiry best-effort ensures the artifact has at least 24 hours until
// expiry. A refresh failure never turns a successful download into a failure.
func (n *NscStore) refreshExpiry(ctx context.Context, client nscAPIClient, key string) {
	err := client.ExtendArtifact(ctx, &storagev1beta.ExtendArtifactRequest{
		Path:          key,
		Namespace:     n.nsc,
		EnsureMinimum: durationpb.New(nscDefaultExpiry),
	})
	if err != nil {
		slog.Warn("failed to refresh cache TTL, continuing (non-fatal)", "key", key, "error", err)
		return
	}
	slog.Debug("refreshed cache TTL", "key", key)
}
