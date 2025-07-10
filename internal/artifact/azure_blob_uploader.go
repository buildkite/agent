package artifact

import (
	"context"
	"fmt"
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

// AzureBlobUploader uploads artifacts to Azure Blob Storage.
type AzureBlobUploader struct {
	// Upload location in Azure Blob Storage.
	loc *AzureBlobLocation

	// Azure Blob storage client.
	client *service.Client

	// The original configuration
	conf AzureBlobUploaderConfig

	// The logger instance to use
	logger logger.Logger
}

// NewAzureBlobUploader creates a new AzureBlobUploader.
func NewAzureBlobUploader(l logger.Logger, c AzureBlobUploaderConfig) (*AzureBlobUploader, error) {
	loc, err := ParseAzureBlobLocation(c.Destination)
	if err != nil {
		return nil, err
	}

	// Initialize the Azure client, and authenticate it
	client, err := NewAzureBlobClient(l, loc.StorageAccountName)
	if err != nil {
		return nil, err
	}

	return &AzureBlobUploader{
		logger: l,
		conf:   c,
		client: client,
		loc:    loc,
	}, nil
}

// URL returns the full destination URL of an artifact.
func (u *AzureBlobUploader) URL(artifact *api.Artifact) string {
	outURL := u.loc.URL(artifact.Path)

	// Generate a shared access signature token for the URL?
	sasDur := os.Getenv("BUILDKITE_AZURE_BLOB_SAS_TOKEN_DURATION")
	if sasDur == "" {
		// no. plain URL.
		return outURL
	}

	dur, err := time.ParseDuration(sasDur)
	if err != nil {
		u.logger.Error("BUILDKITE_AZURE_BLOB_SAS_TOKEN_DURATION is not a valid duration: %v", err)
		return outURL
	}

	fullPath := path.Join(u.loc.BlobPath, artifact.Path)
	blobClient := u.client.NewContainerClient(u.loc.ContainerName).NewBlobClient(fullPath)
	perms := sas.BlobPermissions{Read: true}
	expiry := time.Now().Add(dur)

	sasURL, err := blobClient.GetSASURL(perms, expiry, nil)
	if err != nil {
		u.logger.Error("Couldn't generate SAS URL for container: %v", err)
		return outURL
	}

	u.logger.Debug("Generated Azure Blob SAS URL %q", sasURL)
	return sasURL
}

func (u *AzureBlobUploader) CreateWork(artifact *api.Artifact) ([]workUnit, error) {
	return []workUnit{&azureBlobUploaderWork{
		AzureBlobUploader: u,
		artifact:          artifact,
	}}, nil
}

type azureBlobUploaderWork struct {
	*AzureBlobUploader
	artifact *api.Artifact
}

func (u *azureBlobUploaderWork) Artifact() *api.Artifact { return u.artifact }

func (u *azureBlobUploaderWork) Description() string {
	return singleUnitDescription(u.artifact)
}

// DoWork uploads an artifact file.
func (u *azureBlobUploaderWork) DoWork(ctx context.Context) (*api.ArtifactPartETag, error) {
	u.logger.Debug("Reading file %q", u.artifact.AbsolutePath)
	f, err := os.Open(u.artifact.AbsolutePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %q (%w)", u.artifact.AbsolutePath, err)
	}
	defer f.Close() //nolint:errcheck // File open for read only.

	blobName := path.Join(u.loc.BlobPath, u.artifact.Path)

	u.logger.Debug("Uploading %s to %s", u.artifact.Path, u.loc.URL(blobName))

	bbc := u.client.NewContainerClient(u.loc.ContainerName).NewBlockBlobClient(blobName)
	_, err = bbc.UploadFile(ctx, f, nil)
	return nil, err
}
