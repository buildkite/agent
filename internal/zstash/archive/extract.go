package archive

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/internal/zstash/internal/trace"
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
		// Zip entry names always use forward slashes (per the zip spec),
		// but mapping.RelativePath comes from filepath.Rel and may use the
		// OS native separator (backslash on Windows). Normalise the mapping
		// to forward slashes for the comparison.
		for _, mapping := range mappings {
			if strings.HasPrefix(file.Name, filepath.ToSlash(mapping.RelativePath)) {
				foundPaths[mapping.Path] = true
				return filepath.Join(mapping.Chroot, file.Name), nil
			}
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
