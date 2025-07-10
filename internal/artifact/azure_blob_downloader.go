package artifact

import (
	"context"
	"os"
	"path"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/buildkite/agent/v3/logger"
)

// AzureBlobUploaderConfig configures AzureBlobDownloader.
type AzureBlobDownloaderConfig struct {
	Path        string
	Repository  string
	Destination string
	Retries     int
	DebugHTTP   bool
	TraceHTTP   bool
}

// AzureBlobDownloader downloads files from Azure Blob storage.
type AzureBlobDownloader struct {
	logger logger.Logger
	conf   AzureBlobDownloaderConfig
}

// NewAzureBlobDownloader creates a new AzureBlobDownloader.
func NewAzureBlobDownloader(l logger.Logger, c AzureBlobDownloaderConfig) *AzureBlobDownloader {
	return &AzureBlobDownloader{
		logger: l,
		conf:   c,
	}
}

// Start starts the download.
func (d *AzureBlobDownloader) Start(ctx context.Context) error {
	loc, err := ParseAzureBlobLocation(d.conf.Repository)
	if err != nil {
		return err
	}

	d.logger.Debug("Azure Blob Storage path: %v", loc)

	client, err := NewAzureBlobClient(d.logger, loc.StorageAccountName)
	if err != nil {
		return err
	}

	f, err := os.Create(d.conf.Path)
	if err != nil {
		return err
	}
	// Best-effort close for cleanup - Close error returned for checking below.
	defer f.Close() //nolint:errcheck

	fullPath := path.Join(loc.BlobPath, d.conf.Path)

	// Show a nice message that we're starting to download the file
	d.logger.Debug("Downloading %s to %s", loc.URL(d.conf.Path), d.conf.Path)

	opts := &azblob.DownloadFileOptions{
		RetryReaderOptionsPerBlock: azblob.RetryReaderOptions{
			MaxRetries: int32(d.conf.Retries),
		},
	}
	bc := client.NewContainerClient(loc.ContainerName).NewBlobClient(fullPath)
	if _, err := bc.DownloadFile(ctx, f, opts); err != nil {
		return err
	}

	return f.Close()
}
