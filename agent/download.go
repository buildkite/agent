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

	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/retry"
)

type Download struct {
	// The logger instance to use
	Logger logger.Logger

	// The HTTP client to use for downloading
	Client http.Client

	// The actual URL to get the file from
	URL string

	// The root directory of the download
	Destination string

	// The relative path that should be preserved in the download folder
	Path string

	// How many times should it retry the download before giving up
	Retries int

	// If failed responses should be dumped to the log
	DebugHTTP bool
}

func (d Download) Start() error {
	return retry.Do(func(s *retry.Stats) error {
		err := d.try()
		if err != nil {
			d.Logger.Warn("Error trying to download %s (%s) %s", d.URL, err, s)
		}
		return err
	}, &retry.Config{Maximum: d.Retries, Interval: 5 * time.Second})
}

func (d Download) try() error {
	// If we're downloading a file with a path of "pkg/foo.txt" to a folder
	// called "pkg", we should merge the two paths together. So, instead of it
	// downloading to: destination/pkg/pkg/foo.txt, it will just download to
	// destination/pkg/foo.txt
	destinationPaths := strings.Split(d.Destination, string(os.PathSeparator))
	downloadPaths := strings.Split(d.Path, string(os.PathSeparator))

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

	targetFile := filepath.Join(finalizedDestination, d.Path)
	targetDirectory, _ := filepath.Split(targetFile)

	// Show a nice message that we're starting to download the file
	d.Logger.Debug("Downloading %s to %s", d.URL, targetFile)

	// Start by downloading the file
	response, err := d.Client.Get(d.URL)
	if err != nil {
		return fmt.Errorf("Error while downloading %s (%T: %v)", d.URL, err, err)
	}
	defer response.Body.Close()

	// Double check the status
	if response.StatusCode/100 != 2 && response.StatusCode/100 != 3 {
		if d.DebugHTTP {
			responseDump, err := httputil.DumpResponse(response, true)
			d.Logger.Debug("\nERR: %s\n%s", err, string(responseDump))
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
		return fmt.Errorf("Error when copying data %s (%T: %v)", d.URL, err, err)
	}

	d.Logger.Info("Successfully downloaded \"%s\" %d bytes", d.Path, bytes)

	return nil
}

type downloadError struct {
	s string
}

func (e *downloadError) Error() string {
	return e.s
}
