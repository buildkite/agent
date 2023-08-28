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
}

// AzureBlobDownloader downloads files from Azure Blob storage.
type AzureBlobDownloader struct {
	l    logger.Logger
	conf AzureBlobDownloaderConfig
}

// NewAzureBlobDownloader creates a new AzureBlobDownloader.
func NewAzureBlobDownloader(l logger.Logger, c AzureBlobDownloaderConfig) *AzureBlobDownloader {
	return &AzureBlobDownloader{
		l:    l,
		conf: c,
	}
}

// Start starts the download.
func (d *AzureBlobDownloader) Start(ctx context.Context) error {
	san, ctr, dir, err := ParseAzureBlobDestination(d.conf.Repository)
	if err != nil {
		return err
	}

	d.l.Debug("Azure Blob storage: storage account name = %q", san)
	d.l.Debug("Azure Blob storage: container = %q", ctr)
	d.l.Debug("Azure Blob storage: virtual directory = %q", dir)

	client, err := NewAzureBlobClient(d.l, san)
	if err != nil {
		return err
	}

	f, err := os.Create(d.conf.Path)
	if err != nil {
		return err
	}
	defer f.Close()

	p := path.Join(dir, d.conf.Path)

	opts := &azblob.DownloadFileOptions{
		RetryReaderOptionsPerBlock: azblob.RetryReaderOptions{
			MaxRetries: int32(d.conf.Retries),
		},
	}
	bc := client.NewContainerClient(ctr).NewBlobClient(p)
	if _, err := bc.DownloadFile(ctx, f, opts); err != nil {
		return err
	}

	return f.Close()
}
