package archive

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/internal/cache/internal/trace"
	"github.com/klauspost/compress/zip"
	"github.com/wolfeidau/quickzip"
	"go.opentelemetry.io/otel/attribute"
)

func ListArchive(ctx context.Context, zipFile *os.File, zipFileLen int64) ([]string, error) {
	_, span := trace.Start(ctx, "ListArchive")
	defer span.End()

	reader, err := zip.NewReader(zipFile, zipFileLen)
	if err != nil {
		return nil, fmt.Errorf("failed to open zip reader: %w", err)
	}

	entries := make([]string, 0, len(reader.File))
	for _, f := range reader.File {
		entries = append(entries, f.Name)
	}

	span.SetAttributes(
		attribute.Int("entryCount", len(entries)),
	)

	return entries, nil
}

// entryMatchesMapping reports whether a zip entry belongs to a mapping,
// matching on whole path components so that e.g. entry "cache2/file" does not
// match the mapping for "cache". Zip entry names always use forward slashes
// (per the zip spec), but relativePath comes from filepath.Rel or user
// configuration and may use the OS native separator or contain "./" or
// trailing-slash noise, so it is cleaned and normalised before comparison.
func entryMatchesMapping(entryName, relativePath string) bool {
	rel := filepath.ToSlash(filepath.Clean(relativePath))
	return entryName == rel || strings.HasPrefix(entryName, rel+"/")
}

func ExtractFiles(ctx context.Context, zipFile *os.File, zipFileLen int64, paths []string) (*ArchiveInfo, error) {
	_, span := trace.Start(ctx, "ExtractFiles")
	defer span.End()

	start := time.Now()

	extract, err := quickzip.NewExtractorFromReader(zipFile, zipFileLen)
	if err != nil {
		return nil, fmt.Errorf("failed to create extractor: %w", err)
	}

	mappings, err := PathsToMappings(paths)
	if err != nil {
		return nil, fmt.Errorf("failed to create mappings: %w", err)
	}

	foundPaths := make(map[string]bool)

	err = extract.ExtractWithPathMapper(ctx, func(file *zip.File) (string, error) {
		for _, mapping := range mappings {
			if !entryMatchesMapping(file.Name, mapping.RelativePath) {
				continue
			}

			// Entry names come from the archive and are untrusted: refuse
			// entries whose cleaned destination (e.g. via "..") would land
			// outside the mapping's target path.
			destination := filepath.Join(mapping.Chroot, filepath.FromSlash(file.Name))
			target := filepath.Join(mapping.Chroot, mapping.RelativePath)
			if !IsUnder(destination, target) {
				return "", fmt.Errorf("archive entry %q escapes target path %q", file.Name, mapping.Path)
			}

			foundPaths[mapping.Path] = true
			return destination, nil
		}

		return "", fmt.Errorf("failed to find path mapping for: %s", file.Name)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to extract zip file: %w", err)
	}

	for _, path := range paths {
		if !foundPaths[path] {
			slog.Warn("requested path not found in archive", "path", path)
		}
	}

	bytesExtracted, countExtracted := extract.Written()

	span.SetAttributes(
		attribute.Int64("zipFileLen", zipFileLen),
		attribute.Int64("fileExtracted", countExtracted),
		attribute.Int64("bytesExtracted", bytesExtracted),
	)

	return &ArchiveInfo{
		ArchivePath:    zipFile.Name(),
		Size:           zipFileLen,
		WrittenBytes:   bytesExtracted,
		WrittenEntries: countExtracted,
		Duration:       time.Since(start),
	}, nil
}
