package artifact

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/internal/agenthttp"
	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/version"
	"github.com/buildkite/roko"
	"github.com/dustin/go-humanize"
)

const (
	headerUserAgent = "User-Agent"
)

// Real umask set by init func in download_unix.go. 0o022 is a common default.
var umask = os.FileMode(0o022)

type DownloadConfig struct {
	// The actual URL to get the file from
	URL string

	// The root directory of the download
	Destination string

	// Optional Headers to append to the request
	Headers http.Header

	// HTTP method to use (default is GET)
	Method string

	// The relative path that should be preserved in the download folder
	Path string

	// How many times should it retry the download before giving up
	Retries int

	// Hexadecimal(SHA256(content)) used to verify the downloaded contents, if not empty
	WantSHA256 string

	// If failed responses should be dumped to the log
	// Standard HTTP options.
	DebugHTTP bool
	TraceHTTP bool
}

type Download struct {
	// The download config
	conf DownloadConfig

	// The logger instance to use
	logger logger.Logger

	// The HTTP client to use for downloading
	client *http.Client
}

func NewDownload(l logger.Logger, client *http.Client, c DownloadConfig) *Download {
	return &Download{
		logger: l,
		client: client,
		conf:   c,
	}
}

func (d Download) Start(ctx context.Context) error {
	return roko.NewRetrier(
		roko.WithMaxAttempts(d.conf.Retries),
		roko.WithStrategy(roko.Constant(5*time.Second)),
	).DoWithContext(ctx, func(r *roko.Retrier) error {
		if err := d.try(ctx); err != nil {
			d.logger.Warn("Error trying to download %s (%s) %s", d.conf.URL, err, r)
			return err
		}
		return nil
	})
}

func targetPath(ctx context.Context, dlPath, destPath string) string {
	dlPath = filepath.Clean(dlPath)

	// If we're downloading a file with a path of "pkg/foo.txt" to a folder
	// called "pkg", we should merge the two paths together. So, instead of it
	// downloading to: destination/pkg/pkg/foo.txt, it will just download to
	// destination/pkg/foo.txt
	destPathComponents := strings.Split(destPath, string(os.PathSeparator))
	dlPathComponents := strings.Split(dlPath, string(os.PathSeparator))

	// If the last component of the destination path matches the first component
	// of the download path, then trim the last component of the destination.
	lastIndex := len(destPathComponents) - 1
	lastDestComponent := destPathComponents[lastIndex]
	if lastDestComponent == dlPathComponents[0] {
		destPathComponents = destPathComponents[:lastIndex]
		destPath = strings.Join(destPathComponents, string(os.PathSeparator))
	}

	if experiments.IsEnabled(ctx, experiments.AllowArtifactPathTraversal) {
		// If allow-artifact-path-traversal is enabled, then we don't need to
		// trim ".." components from dlPath before joining.
		return filepath.Join(destPath, dlPath)
	}

	// Trim empty or ".." components from the prefix of dlPath; walking up
	// the directory tree from destPath should be prohibited.
	for len(dlPathComponents) > 0 {
		if c := dlPathComponents[0]; c != "" && c != ".." {
			break
		}
		dlPathComponents = dlPathComponents[1:]
	}
	dlPath = filepath.Join(dlPathComponents...)

	// Join the paths together.
	return filepath.Join(destPath, dlPath)
}

func (d Download) try(ctx context.Context) error {
	targetPath := targetPath(ctx, d.conf.Path, d.conf.Destination)
	targetDirectory, targetFile := filepath.Split(targetPath)

	// Show a nice message that we're starting to download the file
	d.logger.Debug("Downloading %s to %s", d.conf.URL, targetPath)

	method := cmp.Or(d.conf.Method, http.MethodGet)

	request, err := http.NewRequestWithContext(ctx, method, d.conf.URL, nil)
	if err != nil {
		return err
	}

	// If no user agent provided, use the agents default [eg buildkite-agent/3.63.0.8023 (linux; amd64)]
	if _, ok := d.conf.Headers[headerUserAgent]; !ok {
		request.Header.Add(headerUserAgent, version.UserAgent())
	}

	for k, vs := range d.conf.Headers {
		for _, v := range vs {
			request.Header.Add(k, v)
		}
	}

	// Start by downloading the file
	response, err := agenthttp.Do(d.logger, d.client, request,
		agenthttp.WithDebugHTTP(d.conf.DebugHTTP),
		agenthttp.WithTraceHTTP(d.conf.TraceHTTP),
	)
	if err != nil {
		return fmt.Errorf("Error while downloading %s (%T: %w)", d.conf.URL, err, err)
	}
	defer response.Body.Close() //nolint:errcheck // Idiomatic response body handling.

	// Double check the status
	if response.StatusCode/100 != 2 && response.StatusCode/100 != 3 {
		return &downloadError{response.Status}
	}

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

	// Create a SHA256 to hash the download as we go.
	hash := sha256.New()
	out := io.MultiWriter(hash, temp)

	// Copy the data to the file (and hash).
	bytes, err := io.Copy(out, response.Body)
	if err != nil {
		return fmt.Errorf("copying data to temp file (%T: %w)", err, err)
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

	gotSHA256 := hex.EncodeToString(hash.Sum(nil))

	// If the downloader was configured with a checksum to check, check it
	if d.conf.WantSHA256 != "" && gotSHA256 != d.conf.WantSHA256 {
		return fmt.Errorf("checksum of downloaded content %s != uploaded checksum %s", gotSHA256, d.conf.WantSHA256)
	}

	// Rename the temp file to its intended name within the same directory.
	// On Unix-like platforms this is generally an "atomic replace".
	// Caveats: https://pkg.go.dev/os#Rename
	if err := os.Rename(temp.Name(), targetPath); err != nil {
		return fmt.Errorf("renaming temp file to target (%T: %w)", err, err)
	}

	d.logger.Info("Successfully downloaded %q %s with SHA256 %s", d.conf.Path, humanize.IBytes(uint64(bytes)), gotSHA256)

	return nil
}

type downloadError struct {
	s string
}

func (e *downloadError) Error() string {
	return e.s
}
