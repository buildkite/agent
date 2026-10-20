package artifact

import (
	"context"
	"path"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/buildkite/agent/v3/logger"
	"github.com/dustin/go-humanize"
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

	d.logger.Debugf("Azure Blob Storage path: %v", loc)

	client, err := NewAzureBlobClient(d.logger, loc.StorageAccountName)
	if err != nil {
		return err
	}

	targetPath := targetPath(ctx, d.conf.Path, d.conf.Destination)

	temp, err := newDownloadFile(ctx, d.conf.Path, d.conf.Destination)
	if err != nil {
		return err
	}
	defer temp.cleanup()

	fullPath := path.Join(loc.BlobPath, d.conf.Path)

	// Show a nice message that we're starting to download the file
	d.logger.Debugf("Downloading %s to %s", loc.URL(d.conf.Path), targetPath)

	opts := &azblob.DownloadFileOptions{
		RetryReaderOptionsPerBlock: azblob.RetryReaderOptions{
			MaxRetries: int32(d.conf.Retries),
		},
	}
	bc := client.NewContainerClient(loc.ContainerName).NewBlobClient(fullPath)
	bytes, err := bc.DownloadFile(ctx, temp.file, opts)
	if err != nil {
		return err
	}

	if err := temp.commit(); err != nil {
		return err
	}

	d.logger.Infof("Successfully downloaded %q %s", d.conf.Path, humanize.IBytes(uint64(bytes)))

	return nil
}
