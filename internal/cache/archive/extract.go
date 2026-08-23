package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/buildkite/agent/v4/internal/cache/internal/trace"
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

// ExtractFiles extracts a cache archive to the given target paths.
func ExtractFiles(ctx context.Context, log *slog.Logger, zipFile *os.File, zipFileLen int64, paths []string) (*ArchiveInfo, error) {
	_, span := trace.Start(ctx, "ExtractFiles")
	defer span.End()

	start := time.Now()

	reader, err := zip.NewReader(zipFile, zipFileLen)
	if err != nil {
		return nil, fmt.Errorf("failed to open zip reader: %w", err)
	}

	manifest, err := readManifest(reader)
	if err != nil {
		return nil, err
	}

	// Normalise the configured target paths so archive destinations can be
	// matched against them regardless of spelling ("~/cache" vs "$HOME/cache").
	configPaths := make([]string, len(paths))
	for i, p := range paths {
		resolved, err := ResolveConfigPath(p)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve config path %q: %w", p, err)
		}
		configPaths[i] = resolved
	}

	// Entries that are not restored (the manifest, and namespaces absent from
	// local config) are routed to a throwaway directory that is removed when we
	// return, since quickzip's path mapper cannot skip an entry outright.
	discardDir, err := os.MkdirTemp("", "cache-extract-discard-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create discard directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(discardDir) }()

	discard := func(name string) string {
		return discardName(discardDir, name)
	}

	extract, err := quickzip.NewExtractorFromReader(zipFile, zipFileLen)
	if err != nil {
		return nil, fmt.Errorf("failed to create extractor: %w", err)
	}

	foundPaths := make(map[string]bool)

	err = extract.ExtractWithPathMapper(ctx, func(file *zip.File) (string, error) {
		if file.Name == ManifestPath {
			return discard(file.Name), nil
		}

		namespace, rest, ok := splitNamespace(file.Name)
		if !ok {
			log.Warn("archive entry has no namespace, skipping", "entry", file.Name)
			return discard(file.Name), nil
		}

		anchor, ok := manifest.Mappings[namespace]
		if !ok {
			log.Warn("archive entry namespace not in manifest, skipping", "entry", file.Name)
			return discard(file.Name), nil
		}

		base, err := resolveAnchor(anchor)
		if err != nil {
			return "", err
		}

		dest := filepath.Clean(filepath.Join(base, filepath.FromSlash(rest)))

		// Containment: an entry must stay within its own anchor. This rejects
		// zip-slip payloads (e.g. "_0/../../etc/passwd") before they escape.
		if !isUnder(dest, base) {
			return "", fmt.Errorf("entry %q escapes its anchor %q", file.Name, base)
		}

		// Restore an entry only if it falls under a configured target; otherwise discard
		// it (as it is a namespace this job didn't ask for). Targets can't overlap here, so an
		// entry matches at most one.
		matched := -1
		for i, cfg := range configPaths {
			if isUnder(dest, cfg) {
				matched = i
				break
			}
		}
		if matched == -1 {
			return discard(file.Name), nil
		}
		foundPaths[paths[matched]] = true

		return dest, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to extract zip file: %w", err)
	}

	for _, path := range paths {
		if !foundPaths[path] {
			log.WarnContext(ctx, "requested path not found in archive", "path", path)
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

// discardName maps a not-restored entry (the manifest, or an unconfigured
// namespace) to an opaque, separator-free path under discardDir. Hashing keeps
// it inside discardDir even if the name contains "/", "\" or ".." segments.
func discardName(discardDir, name string) string {
	sum := sha256.Sum256([]byte(name))
	return filepath.Join(discardDir, hex.EncodeToString(sum[:]))
}

// splitNamespace splits an entry name into its leading namespace ("_0") and
// the anchor-relative remainder. Zip entry names always use forward slashes.
func splitNamespace(name string) (namespace, rest string, ok bool) {
	i := strings.IndexByte(name, '/')
	if i <= 0 || i == len(name)-1 {
		return "", "", false
	}
	return name[:i], name[i+1:], true
}

// isUnder reports whether p is root itself or a path beneath it.
func isUnder(p, root string) bool {
	if p == root {
		return true
	}
	// A root that already ends in a separator — the POSIX root "/" or a Windows
	// volume root like "C:\" — needs no extra separator. Otherwise require one
	// so "/home/user2" isn't treated as under "/home/user".
	if strings.HasSuffix(root, string(filepath.Separator)) {
		return strings.HasPrefix(p, root)
	}
	return strings.HasPrefix(p, root+string(filepath.Separator))
}
