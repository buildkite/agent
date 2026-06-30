package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Blob interface defines the operations for blob storage
type Blob interface {
	// Upload uploads a file to blob storage
	Upload(ctx context.Context, filePath, key string) (*TransferInfo, error)

	// Download downloads a file from blob storage. expiresAt is the blob's
	// expiry time from the retrieve response (zero if unknown); stores may use
	// it to decide whether to refresh the object's lifecycle expiration.
	Download(ctx context.Context, key, destPath string, expiresAt time.Time) (*TransferInfo, error)
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
