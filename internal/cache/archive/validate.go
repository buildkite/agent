package archive

import (
	"fmt"
	"os"
)

// Validate reports whether a downloaded archive is a readable v2 archive,
// returning ErrUnrecognizedFormat otherwise. It does not verify the content
// digest; integrity checking is handled separately, closer to the download so
// it can hash the bytes in a single pass rather than re-reading the file here.
func Validate(archiveFile string, archiveSize int64) error {
	f, err := os.Open(archiveFile)
	if err != nil {
		return fmt.Errorf("failed to open archive file: %w", err)
	}
	defer func() { _ = f.Close() }()

	return DetectFormat(f, archiveSize)
}
