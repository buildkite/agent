package shell

import (
	"fmt"
	"io/fs"
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
		// Actual file permissions will be reduced by umask, and won't be 0777 unless the user has manually changed the umask to 000
		if err = os.MkdirAll(tempDir, 0777); err != nil {
			return nil, err
		}
	}

	// Create the file
	tempFile, err := os.CreateTemp(tempDir, basename+"-")
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

// SystemTempFileWithExtensionAndMode creates a temporary file with the same extension of the
// provided `filename` and sets its permissions to `perm`. The file will be created in the
// subdirectory `dir` inside the system temp directory.
func SystemTempFileWithExtensionAndMode(dir, filename string, perm fs.FileMode) (*os.File, error) {
	extension := filepath.Ext(filename)
	basename := strings.TrimSuffix(filename, extension)
	tmpName := basename
	if extension != "" {
		tmpName += "-*-" + extension
	}
	tempDir := filepath.Join(os.TempDir(), dir)

	// Best effort ensure tempDir exists
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		// umask will make perms more reasonable
		if err := os.MkdirAll(tempDir, 0o777); err != nil {
			return nil, err
		}
	}

	tempFile, err := os.CreateTemp(tempDir, tmpName)
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary file %q: %w", filename, err)
	}

	if err := tempFile.Chmod(perm); err != nil {
		return nil, fmt.Errorf("failed to chmod temporary file %q: %w", tempFile.Name(), err)
	}

	return tempFile, nil
}

// ClosedSytemTempFileWithExtensionAndMode creates a temporary file with the same extension of the
// provided `filename` and sets its permissions to `perm`. The file will be created in the
// subdirectory `dir` inside the system temp directory. It closes the file after creating it.
func ClosedTempFileWithExtensionAndMode(dir, filename string, perm fs.FileMode) (string, error) {
	f, err := SystemTempFileWithExtensionAndMode(dir, filename, perm)
	if err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}
