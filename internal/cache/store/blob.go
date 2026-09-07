package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrBlobNotFound is returned by a Blob's Download when the requested object
// does not exist in the backing store.
var ErrBlobNotFound = errors.New("blob not found")

// Blob interface defines the operations for blob storage
type Blob interface {
	// Upload uploads a file to blob storage. retention is how long the blob
	// should be kept; a store that has no retention concept (or a zero/negative
	// value, meaning the server didn't specify one) uses its own default.
	Upload(ctx context.Context, filePath, key string, retention time.Duration) (*TransferInfo, error)

	// Download downloads a file from blob storage
	Download(ctx context.Context, key, destPath string) (*TransferInfo, error)
}

// RetentionRefresher is implemented by Blob stores that support extending a
// blob's effective retention/TTL on access (NscStore, S3Blob). Optional
// because not every store has a retention concept to refresh (LocalFileBlob
// does not implement it). retention is the minimum lifetime to guarantee from
// now; a zero/negative value falls back to the store's own default.
type RetentionRefresher interface {
	RefreshRetention(ctx context.Context, key string, retention time.Duration)
}

func NewBlobStore(ctx context.Context, store, bucketURL string) (Blob, error) {
	switch store {
	case AgentManaged:
		scheme, _, _ := strings.Cut(bucketURL, "://")
		switch scheme {
		case nscScheme:
			return NewNscStore(bucketURL)
		case "file":
			// Supported only for local testing, kept consistent with validateCacheStore.
			return NewLocalFileBlob(ctx, bucketURL)
		default:
			return NewS3Blob(ctx, bucketURL)
		}
	case LocalFileStore:
		return NewLocalFileBlob(ctx, bucketURL)
	default:
		return nil, fmt.Errorf("unsupported store type: %s", store)
	}
}
