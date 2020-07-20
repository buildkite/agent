package agent

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/retry"
)

type DownloadConfig struct {
	// The actual URL to get the file from
	URL string

	// The root directory of the download
	Destination string

	// Optional Headers to append to the request
	Headers map[string]string

	// The relative path that should be preserved in the download folder
	Path string

	// How many times should it retry the download before giving up
	Retries int

	// If failed responses should be dumped to the log
	DebugHTTP bool
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

func (d Download) Start() error {
	return retry.Do(func(s *retry.Stats) error {
		err := d.try()
		if err != nil {
			d.logger.Warn("Error trying to download %s (%s) %s", d.conf.URL, err, s)
		}
		return err
	}, &retry.Config{Maximum: d.conf.Retries, Interval: 5 * time.Second})
}

func (d Download) try() error {
	// If we're downloading a file with a path of "pkg/foo.txt" to a folder
	// called "pkg", we should merge the two paths together. So, instead of it
	// downloading to: destination/pkg/pkg/foo.txt, it will just download to
	// destination/pkg/foo.txt
	destinationPaths := strings.Split(d.conf.Destination, string(os.PathSeparator))
	downloadPaths := strings.Split(d.conf.Path, string(os.PathSeparator))

	for i := 0; i < len(downloadPaths); i += 100 {
		// If the last part of the destination path matches
		// this path in the download, then cut it out.
		lastIndex := len(destinationPaths) - 1

		// Break if we've gone too far.
		if lastIndex == -1 {
			break
		}

		lastPathInDestination := destinationPaths[lastIndex]
		if lastPathInDestination == downloadPaths[i] {
			destinationPaths = destinationPaths[:lastIndex]
		}
	}

	finalizedDestination := strings.Join(destinationPaths, string(os.PathSeparator))

	targetFile := filepath.Join(finalizedDestination, d.conf.Path)
	targetDirectory, _ := filepath.Split(targetFile)

	// Show a nice message that we're starting to download the file
	d.logger.Debug("Downloading %s to %s", d.conf.URL, targetFile)

	request, err := http.NewRequest("GET", d.conf.URL, nil)
	if err != nil {
		return err
	}
	for k, v := range d.conf.Headers {
		request.Header.Add(k, v)
	}

	// Start by downloading the file
	response, err := d.client.Do(request)
	if err != nil {
		return fmt.Errorf("Error while downloading %s (%T: %v)", d.conf.URL, err, err)
	}
	defer response.Body.Close()

	// Double check the status
	if response.StatusCode/100 != 2 && response.StatusCode/100 != 3 {
		if d.conf.DebugHTTP {
			responseDump, err := httputil.DumpResponse(response, true)
			if err != nil {
				d.logger.Debug("\nERR: %s\n%s", err, string(responseDump))
			}  else {
				d.logger.Debug("\n%s", string(responseDump))
			}
		}

		return &downloadError{response.Status}
	}

	// Now make the folder for our file
	err = os.MkdirAll(targetDirectory, 0777)
	if err != nil {
		return fmt.Errorf("Failed to create folder for %s (%T: %v)", targetFile, err, err)
	}

	// Create a file to handle the file
	fileBuffer, err := os.Create(targetFile)
	if err != nil {
		return fmt.Errorf("Failed to create file %s (%T: %v)", targetFile, err, err)
	}
	defer fileBuffer.Close()

	// Copy the data to the file
	bytes, err := io.Copy(fileBuffer, response.Body)
	if err != nil {
		return fmt.Errorf("Error when copying data %s (%T: %v)", d.conf.URL, err, err)
	}

	d.logger.Info("Successfully downloaded \"%s\" %d bytes", d.conf.Path, bytes)

	return nil
}

type downloadError struct {
	s string
}

func (e *downloadError) Error() string {
	return e.s
}
