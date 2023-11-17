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
	return SystemTempFileWithExtensionAndMode("", filename, 0o600)
}

// SystemTempFileWithExtensionAndMode creates a temporary file with the same extension of the
// provided `filename` and sets its permissions to `perm`. The file will be created in the
// subdirectory `dir` inside the system temp directory.
func SystemTempFileWithExtensionAndMode(dir, filename string, perm fs.FileMode) (*os.File, error) {
	extension := filepath.Ext(filename)
	basename := strings.TrimSuffix(filename, extension)
	tempDir := filepath.Join(os.TempDir(), dir)

	// Best effort ensure tempDir exists
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		// umask will make perms more reasonable
		if err := os.MkdirAll(tempDir, 0o777); err != nil {
			return nil, err
		}
	}

	tempFile, err := os.CreateTemp(tempDir, basename+"-*"+extension)
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary file %q: %w", filename, err)
	}

	if err := tempFile.Chmod(perm); err != nil {
		return nil, fmt.Errorf("failed to chmod temporary file %q: %w", tempFile.Name(), err)
	}

	return tempFile, nil
}

// ClosedSystemTempFileWithExtensionAndMode creates a temporary file with the same extension of the
// provided `filename` and sets its permissions to `perm`. The file will be created in the
// subdirectory `dir` inside the system temp directory. It closes the file after creating it.
func ClosedSystemTempFileWithExtensionAndMode(dir, filename string, perm fs.FileMode) (string, error) {
	f, err := SystemTempFileWithExtensionAndMode(dir, filename, perm)
	if err != nil {
		return "", err
	}
	return f.Name(), f.Close()
}
