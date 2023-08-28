package artifact

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/service"
	"github.com/buildkite/agent/v3/logger"
)

// The domain suffix for Azure Blob storage.
const azureBlobHostSuffix = ".blob.core.windows.net"

// NewAzureBlobClient creates a new Azure Blob client.
func NewAzureBlobClient(l logger.Logger, storageAccountName string) (*service.Client, error) {
	url := fmt.Sprintf("https://%s%s/", storageAccountName, azureBlobHostSuffix)

	// TODO: other credentials?

	if connStr := os.Getenv("BUILDKITE_AZURE_BLOB_CONNECTION_STRING"); connStr != "" {
		l.Debug("Connecting to Azure Blob Storage using Connection String")
		client, err := service.NewClientFromConnectionString(connStr, nil)
		if err != nil {
			return nil, fmt.Errorf("creating Azure Blob storage client with connection string: %w", err)
		}
		return client, nil
	}

	if accKey := os.Getenv("BUILDKITE_AZURE_BLOB_ACCOUNT_KEY"); accKey != "" {
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

// ParseAzureBlobDestination parses a destination as a URL into a storage
// account name, container name, and remaining path.
func ParseAzureBlobDestination(destination string) (san, ctr, path string, err error) {
	u, err := url.Parse(destination)
	if err != nil {
		return "", "", "", fmt.Errorf("parsing destination: %w", err)
	}
	san, ok := strings.CutSuffix(u.Host, azureBlobHostSuffix)
	if !ok {
		return "", "", "", fmt.Errorf("parsing destination: want subdomain of %s, got %q", azureBlobHostSuffix, u.Host)
	}
	ctr, path, ok = strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	if !ok {
		return "", "", "", fmt.Errorf("parsing destination: want container name as first segment of path, got %q", u.Path)
	}
	return san, ctr, path, nil
}

// IsAzureBlobPath reports if the destination is an Azure Blob storage path.
func IsAzureBlobPath(destination string) bool {
	_, _, _, err := ParseAzureBlobDestination(destination)
	return err == nil
}
