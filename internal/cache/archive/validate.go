package archive

import (
	"fmt"
	"os"

	"github.com/klauspost/compress/zip"
)

// Validate reports whether a downloaded archive is a readable v2 archive,
// returning ErrUnrecognizedFormat otherwise. It checks that the manifest is
// present and readable — not that the entry bodies are intact. Content-digest
// verification is out of scope here.
func Validate(archiveFile string, archiveSize int64) error {
	f, err := os.Open(archiveFile)
	if err != nil {
		return fmt.Errorf("failed to open archive file: %w", err)
	}
	defer func() { _ = f.Close() }()

	reader, err := zip.NewReader(f, archiveSize)
	if err != nil {
		return fmt.Errorf("failed to open zip reader: %w", err)
	}
	_, err = readManifest(reader)
	return err
}
