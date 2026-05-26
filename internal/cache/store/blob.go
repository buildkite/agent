package store

import (
	"context"
	"fmt"
)

// Blob interface defines the operations for blob storage
type Blob interface {
	// Upload uploads a file to blob storage
	Upload(ctx context.Context, filePath, key string) (*TransferInfo, error)

	// Download downloads a file from blob storage
	Download(ctx context.Context, key, destPath string) (*TransferInfo, error)
}

func NewBlobStore(ctx context.Context, store, bucketURL string) (Blob, error) {
	switch store {
	case LocalS3Store:
		return NewS3Blob(ctx, bucketURL)
	case LocalHostedAgents:
		return NewNscStore()
	case LocalFileStore:
		return NewLocalFileBlob(ctx, bucketURL)
	default:
		return nil, fmt.Errorf("unsupported store type: %s", store)
	}
}
