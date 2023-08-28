package artifact

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
)

// AzureBlobUploaderConfig configures AzureBlobUploader.
type AzureBlobUploaderConfig struct {
	// The destination which includes the storage account name and the path.
	// For example, "https://my-storage-account.blob.core.windows.net/my-container/my-virtual-directory/artifacts-go-here/"
	Destination string
}

// AzureBlobUploader uploads artifacts to Azure Blob storage.
type AzureBlobUploader struct {
	// The storage account name set from the destination
	StorageAccountName string

	// Container name, set from the destination.
	ContainerName string

	// The virtual directory path, set from the destination
	BlobPath string

	// Azure Blob storage client.
	client *azblob.Client

	// The original configuration
	conf AzureBlobUploaderConfig

	// The logger instance to use
	logger logger.Logger
}

// NewAzureBlobUploader creates a new AzureBlobUploader.
func NewAzureBlobUploader(l logger.Logger, c AzureBlobUploaderConfig) (*AzureBlobUploader, error) {
	storageAccountName, container, blobPath, err := ParseAzureBlobDestination(c.Destination)
	if err != nil {
		return nil, err
	}

	// Initialize the Azure client, and authenticate it
	client, err := NewAzureBlobClient(l, storageAccountName)
	if err != nil {
		return nil, err
	}

	return &AzureBlobUploader{
		logger:             l,
		conf:               c,
		client:             client,
		StorageAccountName: storageAccountName,
		ContainerName:      container,
		BlobPath:           blobPath,
	}, nil
}

// URL returns the full destination URL of an artifact.
func (u *AzureBlobUploader) URL(artifact *api.Artifact) string {
	return (&url.URL{
		Scheme: "https",
		Host:   u.StorageAccountName + azureBlobHostSuffix,
		Path:   path.Join(u.ContainerName, u.BlobPath, artifact.Path),
	}).String()
}

// Upload uploads an artifact file.
func (u *AzureBlobUploader) Upload(ctx context.Context, artifact *api.Artifact) error {
	u.logger.Debug("Reading file %q", artifact.AbsolutePath)
	f, err := os.Open(artifact.AbsolutePath)
	if err != nil {
		return fmt.Errorf("failed to open file %q (%v)", artifact.AbsolutePath, err)
	}
	defer f.Close()

	blobName := path.Join(u.BlobPath, artifact.Path)

	u.logger.Debug("Uploading %q to container %q path %q", artifact.Path, u.ContainerName, u.BlobPath)
	_, err = u.client.UploadFile(ctx, u.ContainerName, blobName, f, nil)
	return err
}
