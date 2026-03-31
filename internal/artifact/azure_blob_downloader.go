package artifact

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/buildkite/agent/v4/logger"
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

	d.logger.Debug("Azure Blob Storage path: %v", loc)

	client, err := NewAzureBlobClient(d.logger, loc.StorageAccountName)
	if err != nil {
		return err
	}

	targetPath := targetPath(ctx, d.conf.Path, d.conf.Destination)
	targetDirectory, targetFile := filepath.Split(targetPath)

	// Now make the folder for our file
	// Actual file permissions will be reduced by umask, and won't be 0o777 unless the user has manually changed the umask to 000
	if err := os.MkdirAll(targetDirectory, 0o777); err != nil {
		return fmt.Errorf("creating directory for %s (%T: %w)", targetPath, err, err)
	}

	// Create a temporary file to write to.
	temp, err := os.CreateTemp(targetDirectory, targetFile)
	if err != nil {
		return fmt.Errorf("creating temp file (%T: %w)", err, err)
	}
	defer os.Remove(temp.Name()) //nolint:errcheck // Best-effort cleanup
	defer temp.Close()           //nolint:errcheck // Best-effort cleanup - primary Close checked below.

	fullPath := path.Join(loc.BlobPath, d.conf.Path)

	// Show a nice message that we're starting to download the file
	d.logger.Debug("Downloading %s to %s", loc.URL(d.conf.Path), targetPath)

	opts := &azblob.DownloadFileOptions{
		RetryReaderOptionsPerBlock: azblob.RetryReaderOptions{
			MaxRetries: int32(d.conf.Retries),
		},
	}
	bc := client.NewContainerClient(loc.ContainerName).NewBlobClient(fullPath)
	bytes, err := bc.DownloadFile(ctx, temp, opts)
	if err != nil {
		return err
	}

	// os.CreateTemp uses 0o600 permissions, but in the past we used os.Create
	// which uses 0x666. Since these are set at open time, they are restricted
	// by umask.
	if err := temp.Chmod(0o666 &^ umask); err != nil {
		return fmt.Errorf("setting file permissions (%T: %w)", err, err)
	}

	// close must succeed for the file to be considered properly written.
	if err := temp.Close(); err != nil {
		return fmt.Errorf("closing temp file (%T: %w)", err, err)
	}

	// Rename the temp file to its intended name within the same directory.
	// On Unix-like platforms this is generally an "atomic replace".
	// Caveats: https://pkg.go.dev/os#Rename
	if err := os.Rename(temp.Name(), targetPath); err != nil {
		return fmt.Errorf("renaming temp file to target (%T: %w)", err, err)
	}

	d.logger.Info("Successfully downloaded %q %s", d.conf.Path, humanize.IBytes(uint64(bytes)))

	return nil
}
