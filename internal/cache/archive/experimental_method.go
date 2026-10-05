package archive

import (
	"context"
	"fmt"
	"os"

	"github.com/klauspost/compress/zip"
)

// Experimental (A-1952): these let the cache client choose the ZIP entry
// method on the real save path, so benchmarks can compare Zstd and Store
// end to end. They are not a customer-facing setting. BuildArchive still
// always uses Zstd, and both methods produce archives that the normal
// extraction path reads.

func parseEntryMethod(name string) (entryMethod, error) {
	switch name {
	case "zstd":
		return entryMethodZstd, nil
	case "store":
		return entryMethodStore, nil
	default:
		return 0, fmt.Errorf("unsupported archive entry method %q: want zstd or store", name)
	}
}

// ValidateEntryMethod returns an error unless name is "zstd" or "store".
func ValidateEntryMethod(name string) error {
	_, err := parseEntryMethod(name)
	return err
}

// BuildArchiveWithMethod is BuildArchive with the ZIP entry method named by
// methodName ("zstd" or "store"). Checksumming and archive layout are the
// same as BuildArchive's.
func BuildArchiveWithMethod(ctx context.Context, paths []string, key, methodName string) (*ArchiveInfo, error) {
	method, err := parseEntryMethod(methodName)
	if err != nil {
		return nil, err
	}
	return buildArchive(ctx, paths, key, method)
}

// CheckEntryMethod returns an error unless every non-empty regular file in
// the archive uses the method named by methodName. Directories, symlinks,
// empty files (always stored) and the manifest don't depend on the method,
// so they are not checked. It returns the number of entries checked.
func CheckEntryMethod(archiveFile string, archiveSize int64, methodName string) (int, error) {
	method, err := parseEntryMethod(methodName)
	if err != nil {
		return 0, err
	}

	f, err := os.Open(archiveFile)
	if err != nil {
		return 0, fmt.Errorf("failed to open archive file: %w", err)
	}
	defer func() { _ = f.Close() }()

	reader, err := zip.NewReader(f, archiveSize)
	if err != nil {
		return 0, fmt.Errorf("failed to open zip reader: %w", err)
	}

	checked := 0
	for _, entry := range reader.File {
		if entry.Name == ManifestPath || !entry.Mode().IsRegular() || entry.UncompressedSize64 == 0 {
			continue
		}
		if entry.Method != uint16(method) {
			return checked, fmt.Errorf("archive entry %q uses ZIP method %d, want %d (%s)", entry.Name, entry.Method, method, methodName)
		}
		checked++
	}
	return checked, nil
}
