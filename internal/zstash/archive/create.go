package archive

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/buildkite/agent/v3/internal/zstash/internal/trace"
	"github.com/klauspost/compress/zstd"
	"github.com/wolfeidau/quickzip"
	"go.opentelemetry.io/otel/attribute"
)

func BuildArchive(ctx context.Context, paths []string, key string) (*ArchiveInfo, error) {
	_, span := trace.Start(ctx, "BuildArchive")
	defer span.End()

	start := time.Now()

	modified, err := time.Parse(time.RFC3339, modifiedEpoch)
	if err != nil {
		return nil, fmt.Errorf("failed to parse modified epoch: %w", err)
	}

	archiveFile, err := os.CreateTemp("", fmt.Sprintf("%s-*.zip", key))
	if err != nil {
		return nil, fmt.Errorf("failed to create archive file: %w", err)
	}
	defer func() {
		_ = archiveFile.Close()
	}()

	checksummer := NewChecksumSHA256(archiveFile)

	// wrap the file in an io.Writer which records the sha256sum of the file
	arc, err := quickzip.NewArchiver(
		checksummer,
		quickzip.WithArchiverMethod(zstd.ZipMethodWinZip),
		quickzip.WithArchiverBufferSize(bufferSize),
		quickzip.WithModifiedEpoch(modified),
		quickzip.WithSkipOwnership(skipOwnership),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create archiver: %w", err)
	}

	mappings, err := PathsToMappings(paths)
	if err != nil {
		return nil, fmt.Errorf("failed to get mappings: %w", err)
	}

	for _, mapping := range mappings {
		_, err := os.Stat(mapping.ResolvedPath)
		if err != nil {
			if os.IsNotExist(err) {
				slog.Warn("file does not exist", "path", mapping.ResolvedPath)
				continue
			}
			return nil, fmt.Errorf("failed to stat file: %w", err)
		}

		_, err = isUnderHome(mapping.ResolvedPath)
		if err != nil {
			return nil, fmt.Errorf("failed directory (%s) outside home directory: %w", mapping.ResolvedPath, err)
		}

		files := make(map[string]os.FileInfo)
		err = filepath.Walk(mapping.ResolvedPath, func(filename string, fi os.FileInfo, err error) error {
			files[filename] = fi
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to walk path: %s with error: %w", mapping.ResolvedPath, err)
		}

		slog.Debug("chroot", "chroot", mapping.Chroot, "path", mapping.ResolvedPath)

		err = arc.Archive(context.Background(), mapping.Chroot, files)
		if err != nil {
			return nil, fmt.Errorf("failed to archive path: %s with error: %w", mapping.ResolvedPath, err)
		}
	}

	writtenBytes, writtenEntries := arc.Written()

	err = arc.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to close archive: %w", err)
	}

	stat, err := archiveFile.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat archive file: %w", err)
	}

	span.SetAttributes(
		attribute.String("Sha256sum", checksummer.Sum()),
		attribute.Int64("Size", stat.Size()),
	)

	return &ArchiveInfo{
		ArchivePath:    archiveFile.Name(),
		Size:           stat.Size(),
		Sha256sum:      checksummer.Sum(),
		WrittenBytes:   writtenBytes,
		WrittenEntries: writtenEntries,
		Duration:       time.Since(start),
	}, nil
}
