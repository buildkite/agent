package reapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/cache/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maxBlobSize = 64 << 20

// Backend uses a job-authenticated API client and the agent's configured store.
// Objects use a versioned ZIP envelope, separate from filesystem cache entries.
type Backend struct {
	API       *api.Client
	Registry  string
	BucketURL string
}

func address(instance, kind, key string) api.CacheEntryRetrieveReq {
	return api.CacheEntryRetrieveReq{
		TargetPaths: []string{".buildkite-reapi-v1"},
		CacheKey: []api.CacheKeyPart{
			{Value: "reapi-v1", Mandatory: true},
			{Value: "instance:" + instance, Mandatory: true},
			{Value: kind, Mandatory: true},
			{Value: key, Mandatory: true},
		},
	}
}

func (b *Backend) registry() string {
	if b.Registry == "" {
		return "~"
	}
	return b.Registry
}

func (b *Backend) get(ctx context.Context, instance, kind, key string) ([]byte, error) {
	entry, exists, response, err := b.API.CacheEntryRetrieve(ctx, b.registry(), address(instance, kind, key))
	if err != nil {
		return nil, apiError(response, err)
	}
	if !exists {
		if diagnostics := entry.RestoreDiagnostics; diagnostics != nil {
			if diagnostics.SearchIncomplete || diagnostics.BudgetExhausted {
				return nil, status.Error(codes.Unavailable, "cache registry search incomplete")
			}
			for _, attempt := range diagnostics.Attempts {
				if attempt.Outcome == "denied" {
					return nil, status.Error(codes.PermissionDenied, "cache registry policy denied access")
				}
			}
		}
		return nil, status.Error(codes.NotFound, "cache entry not found")
	}
	if len(entry.Blobs) != 1 || entry.Blobs[0].Compression != "zip" || entry.Blobs[0].Digest.Algorithm != "sha256" || entry.Blobs[0].FileSize > maxBlobSize+(1<<20) || entry.Blobs[0].FileSize < 0 {
		return nil, status.Error(codes.DataLoss, "invalid REAPI envelope metadata")
	}
	blob, err := store.NewBlobStore(ctx, entry.Store, b.BucketURL)
	if err != nil {
		return nil, backendError(err)
	}
	f, err := os.CreateTemp("", "buildkite-reapi-*")
	if err != nil {
		return nil, backendError(err)
	}
	name := f.Name()
	defer os.Remove(name) //nolint:errcheck
	if err := f.Close(); err != nil {
		return nil, backendError(err)
	}
	if _, err := blob.Download(ctx, entry.Blobs[0].Digest.Value, name); err != nil {
		if errors.Is(err, store.ErrBlobNotFound) {
			return nil, status.Error(codes.NotFound, "cache blob not found")
		}
		return nil, backendError(err)
	}
	f, err = os.Open(name)
	if err != nil {
		return nil, backendError(err)
	}
	defer f.Close() //nolint:errcheck
	data, err := io.ReadAll(io.LimitReader(f, maxBlobSize+(1<<20)+1))
	if err != nil {
		return nil, backendError(err)
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != entry.Blobs[0].FileSize || hex.EncodeToString(sum[:]) != entry.Blobs[0].Digest.Value {
		return nil, status.Error(codes.DataLoss, "REAPI envelope digest mismatch")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(zr.File) != 1 || zr.File[0].Name != "payload" || zr.File[0].UncompressedSize64 > maxBlobSize {
		return nil, status.Error(codes.DataLoss, "invalid REAPI envelope")
	}
	r, err := zr.File[0].Open()
	if err != nil {
		return nil, status.Error(codes.DataLoss, "invalid REAPI payload")
	}
	defer r.Close() //nolint:errcheck
	payload, err := io.ReadAll(io.LimitReader(r, maxBlobSize+1))
	if err != nil || len(payload) > maxBlobSize {
		return nil, status.Error(codes.DataLoss, "invalid REAPI payload")
	}
	if !entry.Fallback {
		if refresher, ok := blob.(store.RetentionRefresher); ok {
			refresher.RefreshRetention(ctx, entry.Blobs[0].Digest.Value, time.Duration(entry.RetentionDays)*24*time.Hour)
		}
		_, response, err = b.API.CacheEntryConfirm(ctx, b.registry(), api.CacheEntryConfirmReq{
			TargetPaths: entry.TargetPaths,
			CacheKey:    entry.CacheKey,
			Scopes:      entry.Scopes,
			UploadID:    entry.UploadID,
		})
		if err != nil {
			return nil, apiError(response, err)
		}
	}
	return payload, nil
}

func (b *Backend) put(ctx context.Context, instance, kind, key string, payload []byte) error {
	if len(payload) > maxBlobSize {
		return status.Error(codes.ResourceExhausted, "blob exceeds 64 MiB limit")
	}
	f, err := os.CreateTemp("", "buildkite-reapi-*")
	if err != nil {
		return backendError(err)
	}
	defer os.Remove(f.Name()) //nolint:errcheck
	defer f.Close()           //nolint:errcheck
	zw := zip.NewWriter(f)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "payload", Method: zip.Store})
	if err != nil {
		return backendError(err)
	}
	if _, err := w.Write(payload); err != nil {
		return backendError(err)
	}
	if err := zw.Close(); err != nil {
		return backendError(err)
	}
	size, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return backendError(err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return backendError(err)
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return backendError(err)
	}
	if err := f.Close(); err != nil {
		return backendError(err)
	}
	registry, response, err := b.API.CacheRegistry(ctx, b.registry())
	if err != nil {
		return apiError(response, err)
	}
	blob, err := store.NewBlobStore(ctx, registry.Store, b.BucketURL)
	if err != nil {
		return backendError(err)
	}
	addr := address(instance, kind, key)
	digest := hex.EncodeToString(h.Sum(nil))
	created, response, err := b.API.CacheEntryCreate(ctx, b.registry(), api.CacheEntryCreateReq{
		TargetPaths: addr.TargetPaths,
		CacheKey:    addr.CacheKey,
		Platform:    "reapi-v1",
		Blobs: []api.CacheBlob{{
			Digest:      api.CacheDigest{Algorithm: "sha256", Value: digest},
			FileSize:    size,
			Compression: "zip",
		}},
	})
	if err != nil {
		return apiError(response, err)
	}
	if _, err := blob.Upload(ctx, f.Name(), digest, time.Duration(created.RetentionDays)*24*time.Hour); err != nil {
		return backendError(err)
	}
	_, response, err = b.API.CacheEntryCommit(ctx, b.registry(), api.CacheEntryCommitReq{UploadID: created.UploadID})
	if err != nil {
		return apiError(response, err)
	}
	return nil
}

func apiError(response *api.Response, err error) error {
	if response != nil {
		switch response.StatusCode {
		case 401:
			return status.Error(codes.Unauthenticated, "cache API authentication failed")
		case 403:
			return status.Error(codes.PermissionDenied, "cache API access denied")
		}
	}
	return backendError(err)
}

func backendError(err error) error {
	if errors.Is(err, context.Canceled) {
		return status.Error(codes.Canceled, "request canceled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, "request timed out")
	}
	// Do not forward backend errors: storage URLs and credentials can appear in them.
	return status.Error(codes.Unavailable, "Buildkite Cache operation failed")
}
