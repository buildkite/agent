package buildbox

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type Download struct {
	// The actual URL to get the file from
	URL string

	// The root directory of the download
	Destination string

	// The relative path that should be preserved in the download folder
	Path string
}

func StartDownload(quit chan string, download Download) {
	// If we're downloading a file with a path of "pkg/foo.txt" to a folder
	// called "pkg", we should merge the two paths together. So, instead of it
	// downloading to: destination/pkg/pkg/foo.txt, it will just download to
	// destination/pkg/foo.txt
	destinationPaths := strings.Split(download.Destination, string(os.PathSeparator))
	downloadPaths := strings.Split(download.Path, string(os.PathSeparator))

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

	targetFile := filepath.Join(finalizedDestination, download.Path)
	targetDirectory, _ := filepath.Split(targetFile)

	// Show a nice message that we're starting to download the file
	Logger.Debugf("Downloading %s to %s", download.URL, targetFile)

	// Start by downloading the file
	response, err := http.Get(download.URL)
	if err != nil {
		Logger.Errorf("Error while downloading %s (%T: %v)", download.URL, err, err)
		return
	}
	defer response.Body.Close()

	// Now make the folder for our file
	err = os.MkdirAll(targetDirectory, 0777)
	if err != nil {
		Logger.Errorf("Failed to create folder for %s (%T: %v)", targetFile, err, err)
		return
	}

	// Create a file to handle the file
	fileBuffer, err := os.Create(targetFile)
	if err != nil {
		Logger.Errorf("Failed to create file %s (%T: %v)", targetFile, err, err)
		return
	}
	defer fileBuffer.Close()

	// Copy the data to the file
	bytes, err := io.Copy(fileBuffer, response.Body)
	if err != nil {
		Logger.Errorf("Error when copying data %s (%T: %v)", download.URL, err, err)
		return
	}

	Logger.Infof("Successfully downloaded %s (%d bytes)", download.Path, bytes)

	// We can notify the channel that this routine has finished now
	quit <- "finished"
}
