package agent

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/buildkite/agent/logger"
)

type Download struct {
	// The actual URL to get the file from
	URL string

	// The root directory of the download
	Destination string

	// The relative path that should be preserved in the download folder
	Path string

	// How many times should it retry the download before giving up
	Retries int
}

func (d Download) Start() error {
	seconds := 5 * time.Second
	ticker := time.NewTicker(seconds)
	retries := 1
	max := d.Retries

	for {
		err := d.try()
		if err == nil {
			break
		}

		if retries >= max {
			break
		} else {
			logger.Warn("Error trying to download %s (%d/%d) (%T: %v) Trying again in %s", d.URL, retries, max, err, err, seconds)
		}

		retries++
		<-ticker.C
	}

	return nil
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
	logger.Debug("Downloading %s to %s", d.URL, targetFile)

	// Start by downloading the file
	response, err := http.Get(d.URL)
	if err != nil {
		return fmt.Errorf("Error while downloading %s (%T: %v)", d.URL, err, err)
	}
	defer response.Body.Close()

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

	logger.Info("Successfully downloaded \"%s\" %d bytes", d.Path, bytes)

	return nil
}
