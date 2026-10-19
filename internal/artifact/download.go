package artifact

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/buildkite/agent/v4/internal/agenthttp"
	"github.com/buildkite/agent/v4/internal/osutil"
	"github.com/buildkite/agent/v4/logger"
	"github.com/buildkite/agent/v4/version"
	"github.com/buildkite/roko"
	"github.com/dustin/go-humanize"
)

const (
	headerUserAgent = "User-Agent"
)

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
			d.logger.Warnf("Error trying to download %s (%s) %s", d.conf.URL, err, r)
			return err
		}
		return nil
	})
}

func targetPath(_ context.Context, dlPath, destPath string) string {
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

// downloadFile keeps directory creation, temporary files, cleanup, and rename
// confined to the requested destination, including when parents are symlinks.
type downloadFile struct {
	file       *os.File
	root       *os.Root
	targetPath string
	tempPath   string
}

func newDownloadFile(ctx context.Context, path, destination string) (_ *downloadFile, err error) {
	// Preserve targetPath's path merging, but root operations must use paths
	// relative to the original destination, not its parent or the target directory.
	target, err := filepath.Rel(cmp.Or(destination, "."), targetPath(ctx, path, destination))
	if err != nil {
		return nil, fmt.Errorf("resolving download path: %w", err)
	}
	root, err := os.OpenRoot(cmp.Or(destination, "."))
	if err != nil {
		return nil, fmt.Errorf("opening download destination: %w", err)
	}
	defer func() {
		if err != nil {
			_ = root.Close()
		}
	}()

	// Permissions are reduced by umask, as with the previous os.MkdirAll.
	if err := root.MkdirAll(filepath.Dir(target), 0o777); err != nil {
		return nil, fmt.Errorf("creating directory for %s: %w", target, err)
	}
	// os.Root has no CreateTemp. Use an unpredictable name and exclusive creation
	// within the root; never reopen a root-relative path with an ordinary os call.
	tempPath := filepath.Join(filepath.Dir(target), ".buildkite-artifact-"+rand.Text())
	file, err := root.OpenFile(tempPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	return &downloadFile{file: file, root: root, targetPath: target, tempPath: tempPath}, nil
}

func (f *downloadFile) cleanup() {
	// Best-effort: after a successful commit, Close and Remove are expected to fail.
	_ = f.file.Close()
	_ = f.root.Remove(f.tempPath)
	_ = f.root.Close()
}

func (f *downloadFile) commit() error {
	// The temp file is private while downloading; give the finished file the
	// usual 0o666 minus umask, since umask isn't applied by Chmod.
	if err := f.file.Chmod(0o666 &^ osutil.Umask); err != nil {
		return fmt.Errorf("setting file permissions: %w", err)
	}
	if err := f.file.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	// Replace the target only after the download and any checksum verification
	// succeed. Both names are in the same directory and relative to the root.
	if err := f.root.Rename(f.tempPath, f.targetPath); err != nil {
		return fmt.Errorf("renaming temp file to target: %w", err)
	}
	return nil
}

func (d Download) try(ctx context.Context) error {
	targetPath := targetPath(ctx, d.conf.Path, d.conf.Destination)

	// Show a nice message that we're starting to download the file
	d.logger.Debugf("Downloading %s to %s", d.conf.URL, targetPath)

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

	temp, err := newDownloadFile(ctx, d.conf.Path, d.conf.Destination)
	if err != nil {
		return err
	}
	defer temp.cleanup()

	// Create a SHA256 to hash the download as we go.
	hash := sha256.New()
	out := io.MultiWriter(hash, temp.file)

	// Copy the data to the file (and hash).
	bytes, err := io.Copy(out, response.Body)
	if err != nil {
		return fmt.Errorf("copying data to temp file (%T: %w)", err, err)
	}

	gotSHA256 := hex.EncodeToString(hash.Sum(nil))

	// If the downloader was configured with a checksum to check, check it
	if d.conf.WantSHA256 != "" && gotSHA256 != d.conf.WantSHA256 {
		return fmt.Errorf("checksum of downloaded content %s != uploaded checksum %s", gotSHA256, d.conf.WantSHA256)
	}

	if err := temp.commit(); err != nil {
		return err
	}

	d.logger.Infof("Successfully downloaded %q %s with SHA256 %s", d.conf.Path, humanize.IBytes(uint64(bytes)), gotSHA256)

	return nil
}

type downloadError struct {
	s string
}

func (e *downloadError) Error() string {
	return e.s
}
