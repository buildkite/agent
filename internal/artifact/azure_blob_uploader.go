package artifact

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
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

	// The virtual directory path, set from the destination.
	BlobPath string

	// Azure Blob storage client.
	client *service.Client

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
	outURL := &url.URL{
		Scheme: "https",
		Host:   u.StorageAccountName + azureBlobHostSuffix,
		Path:   path.Join(u.ContainerName, u.BlobPath, artifact.Path),
	}

	// Generate a shared access signature token for the URL?
	sasDur := os.Getenv("BUILDKITE_AZURE_BLOB_SAS_TOKEN_DURATION")
	if sasDur == "" {
		// no. plain URL.
		return outURL.String()
	}

	dur, err := time.ParseDuration(sasDur)
	if err != nil {
		u.logger.Error("BUILDKITE_AZURE_BLOB_SAS_TOKEN_DURATION is not a valid duration: %v", err)
		return outURL.String()
	}

	bc := u.client.NewContainerClient(u.ContainerName).NewBlobClient(path.Join(u.BlobPath, artifact.Path))
	perms := sas.BlobPermissions{Read: true}
	expiry := time.Now().Add(dur)
	sasURL, err := bc.GetSASURL(perms, expiry, nil)
	if err != nil {
		u.logger.Error("Couldn't generate SAS URL for container: %v", err)
		return outURL.String()
	}

	u.logger.Info("Generated Blob SAS URL %q", sasURL)
	return sasURL
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
	bbc := u.client.NewContainerClient(u.ContainerName).NewBlockBlobClient(blobName)
	_, err = bbc.UploadFile(ctx, f, nil)
	return err
}
