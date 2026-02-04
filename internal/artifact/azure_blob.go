package artifact

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
	"github.com/buildkite/agent/v3/logger"
)

// The domain suffix for Azure Blob storage.
const azureBlobHostSuffix = ".blob.core.windows.net"

// NewAzureBlobClient creates a new Azure Blob Storage client.
func NewAzureBlobClient(l logger.Logger, storageAccountName string) (*service.Client, error) {
	// TODO: Other credential types?
	// https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/azidentity#readme-credential-types

	if connStr := os.Getenv("BUILDKITE_AZURE_BLOB_CONNECTION_STRING"); connStr != "" {
		l.Debug("Connecting to Azure Blob Storage using Connection String")
		client, err := service.NewClientFromConnectionString(connStr, nil)
		if err != nil {
			return nil, fmt.Errorf("creating Azure Blob storage client with connection string: %w", err)
		}
		return client, nil
	}

	url := fmt.Sprintf("https://%s%s/", storageAccountName, azureBlobHostSuffix)

	if accKey := os.Getenv("BUILDKITE_AZURE_BLOB_ACCESS_KEY"); accKey != "" {
		l.Debug("Connecting to Azure Blob Storage using Shared Key Credential")
		cred, err := service.NewSharedKeyCredential(storageAccountName, accKey)
		if err != nil {
			return nil, fmt.Errorf("creating Azure shared key credential: %w", err)
		}
		client, err := service.NewClientWithSharedKeyCredential(url, cred, nil)
		if err != nil {
			return nil, fmt.Errorf("creating Azure Blob storage client with a shared key credential: %w", err)
		}
		return client, nil
	}

	l.Debug("Connecting to Azure Blob Storage using Default Azure Credential")
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("creating default Azure credential: %w", err)
	}

	client, err := service.NewClient(url, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure Blob storage client with default Azure credential: %w", err)
	}
	return client, nil
}

// AzureBlobLocation specifies the location of a blob in Azure Blob Storage.
type AzureBlobLocation struct {
	StorageAccountName string
	ContainerName      string
	BlobPath           string
}

// URL returns an Azure Blob Storage URL for the blob.
func (l *AzureBlobLocation) URL(blob string) string {
	return (&url.URL{
		Scheme: "https",
		Host:   l.StorageAccountName + azureBlobHostSuffix,
		Path:   path.Join(l.ContainerName, l.BlobPath, blob),
	}).String()
}

// String returns the location as a URL string.
func (l *AzureBlobLocation) String() string {
	return l.URL("")
}

// ParseAzureBlobLocation parses a URL into an Azure Blob Storage location.
func ParseAzureBlobLocation(loc string) (*AzureBlobLocation, error) {
	u, err := url.Parse(loc)
	if err != nil {
		return nil, fmt.Errorf("parsing location: %w", err)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("parsing location: want https:// scheme, got %q", u.Scheme)
	}
	san, ok := strings.CutSuffix(u.Host, azureBlobHostSuffix)
	if !ok {
		return nil, fmt.Errorf("parsing location: want subdomain of %s, got %q", azureBlobHostSuffix, u.Host)
	}
	ctr, blob, ok := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	if !ok {
		return nil, fmt.Errorf("parsing location: want container name as first segment of path, got %q", u.Path)
	}
	return &AzureBlobLocation{
		StorageAccountName: san,
		ContainerName:      ctr,
		BlobPath:           blob,
	}, nil
}

// IsAzureBlobPath reports if the location is an Azure Blob Storage path.
func IsAzureBlobPath(loc string) bool {
	_, err := ParseAzureBlobLocation(loc)
	return err == nil
}
