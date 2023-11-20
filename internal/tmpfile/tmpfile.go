package tmpfile

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	userOnlyRW = 0o600
	allRWX     = 0o777
)

// KeepExtension creates a temporary file that with the same extension as the provided `filename`.
// It will be created in the subdirectory "buildkite-agent" inside the system temp directory.
func KeepExtension(filename string) (*os.File, error) {
	return KeepExtensionWithMode("buildkite-agent", filename, userOnlyRW)
}

// KeepExtensionAndClose creates a temporary file that with the same extension as the provided
// filename and closes it. It will be created in the subdirectory `dir` inside the system temp
// directory.
func KeepExtensionAndClose(dir, filename string) (string, error) {
	return KeepExtensionWithModeAndClose(dir, filename, userOnlyRW)
}

// KeepExtensionWithMode creates a temporary file with the same extension as the provided `filename`
// and sets its permissions to `perm`. The file will be created in the subdirectory `dir` inside the
// system temp directory.
func KeepExtensionWithMode(dir, filename string, perm fs.FileMode) (*os.File, error) {
	extension := filepath.Ext(filename)
	basename := strings.TrimSuffix(filename, extension)
	tempDir := filepath.Join(os.TempDir(), dir)

	// umask will make perms more reasonable
	if err := os.MkdirAll(tempDir, allRWX); err != nil {
		return nil, fmt.Errorf("failed to create temporary directory %q: %w", tempDir, err)
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

// KeepExtensionWithModeAndClose creates a temporary file with the same extension of the
// provided `filename` and sets its permissions to `perm`. The file will be created in the
// subdirectory `dir` inside the system temp directory. It closes the file after creating it.
func KeepExtensionWithModeAndClose(dir, filename string, perm fs.FileMode) (string, error) {
	f, err := KeepExtensionWithMode(dir, filename, perm)
	if err != nil {
		return "", err
	}
	return f.Name(), f.Close()
}
