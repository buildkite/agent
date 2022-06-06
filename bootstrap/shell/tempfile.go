package shell

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

// TempFileWithExtension creates a temporary file that copies the extension of the provided filename
func TempFileWithExtension(filename string) (*os.File, error) {
	extension := filepath.Ext(filename)
	basename := strings.TrimSuffix(filename, extension)

	// TempDir is not guaranteed to exist
	tempDir := os.TempDir()
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		if err = os.MkdirAll(tempDir, 0777); err != nil {
			return nil, err
		}
	}

	// Create the file
	tempFile, err := ioutil.TempFile(tempDir, basename+"-")
	if err != nil {
		return nil, fmt.Errorf("Failed to create temporary file \"%s\" (%s)", filename, err)
	}

	// Do we need to rename the file?
	if extension != "" {
		// Close the currently open tempfile
		tempFile.Close()

		// Rename it
		newTempFileName := tempFile.Name() + extension
		err = os.Rename(tempFile.Name(), newTempFileName)
		if err != nil {
			return nil, fmt.Errorf("Failed to rename \"%s\" to \"%s\" (%s)", tempFile.Name(), newTempFileName, err)
		}

		// Open it again
		tempFile, err = os.OpenFile(newTempFileName, os.O_RDWR|os.O_EXCL, 0600)
		if err != nil {
			return nil, fmt.Errorf("Failed to open temporary file \"%s\" (%s)", newTempFileName, err)
		}
	}

	return tempFile, nil
}
