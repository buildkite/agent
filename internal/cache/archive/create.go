package archive

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/buildkite/agent/v4/internal/cache/internal/trace"
	"github.com/klauspost/compress/zip"
	"github.com/klauspost/compress/zstd"
	"github.com/wolfeidau/quickzip"
	"go.opentelemetry.io/otel/attribute"
)

// BuildArchive builds a cache archive for the given target paths.
func BuildArchive(ctx context.Context, log *slog.Logger, paths []string, key string) (*ArchiveInfo, error) {
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

	// Wrap the file in an io.Writer which records the sha256sum of the archive.
	checksummer := NewChecksumSHA256(archiveFile)
	zw := zip.NewWriter(checksummer)

	mappings, err := PathsToMappings(paths)
	if err != nil {
		return nil, fmt.Errorf("failed to get mappings: %w", err)
	}

	// Resolve each mapping's on-disk layout, skipping paths that don't exist.
	// The manifest records only the namespaces actually written.
	type plan struct {
		mapping Mapping
		chroot  string
		prefix  string
	}
	plans := make([]plan, 0, len(mappings))
	manifest := Manifest{Version: ManifestVersion, Mappings: make(map[string]string, len(mappings))}

	for _, mapping := range mappings {
		if _, err := os.Stat(mapping.ResolvedPath()); err != nil {
			if os.IsNotExist(err) {
				log.WarnContext(ctx, "cache path does not exist, skipping", "path", mapping.Path, "resolved", mapping.ResolvedPath())
				continue
			}
			return nil, fmt.Errorf("failed to stat file: %w", err)
		}

		chroot, prefix := mapping.archiveLayout()

		plans = append(plans, plan{mapping: mapping, chroot: chroot, prefix: prefix})
		manifest.Mappings[mapping.Namespace] = mapping.Anchor
	}

	// Reject overlapping targets: they archive shared files twice and collide on
	// one restore destination. Restore re-checks the re-resolved paths.
	resolved := make([]string, len(plans))
	for i, p := range plans {
		resolved[i] = p.mapping.ResolvedPath()
	}
	if i, j, ok := OverlappingPaths(resolved); ok {
		return nil, fmt.Errorf("cache target_paths overlap: %q and %q resolve to nested or aliased locations; remove the redundant one", plans[i].mapping.Path, plans[j].mapping.Path)
	}

	if err := writeManifest(zw, manifest); err != nil {
		return nil, err
	}

	var writtenBytes, writtenEntries int64
	for _, p := range plans {
		b, e, err := archiveMapping(ctx, zw, p.mapping.ResolvedPath(), p.chroot, p.prefix, modified)
		if err != nil {
			return nil, fmt.Errorf("failed to archive path %q: %w", p.mapping.Path, err)
		}
		writtenBytes += b
		writtenEntries += e
	}

	if err := zw.Close(); err != nil {
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

// archiveMapping archives a single target path into a temporary zip via
// quickzip, then copies each entry into the final archive with its namespace prefix.
// Entries are copied raw (already compressed), so there is no second compression pass.
func archiveMapping(ctx context.Context, zw *zip.Writer, resolvedPath, chroot, prefix string, modified time.Time) (writtenBytes, writtenEntries int64, err error) {
	tmp, err := os.CreateTemp("", "cache-ns-*.zip")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create namespace archive: %w", err)
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	arc, err := quickzip.NewArchiver(
		tmp,
		quickzip.WithArchiverMethod(zstd.ZipMethodWinZip),
		quickzip.WithArchiverBufferSize(bufferSize),
		quickzip.WithModifiedEpoch(modified),
		quickzip.WithSkipOwnership(skipOwnership),
	)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create archiver: %w", err)
	}

	files := make(map[string]os.FileInfo)
	err = filepath.Walk(resolvedPath, func(filename string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		files[filename] = fi
		return nil
	})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to walk path %q: %w", resolvedPath, err)
	}

	if err := arc.Archive(ctx, chroot, files); err != nil {
		return 0, 0, fmt.Errorf("failed to archive path %q: %w", resolvedPath, err)
	}
	writtenBytes, writtenEntries = arc.Written()
	if err := arc.Close(); err != nil {
		return 0, 0, fmt.Errorf("failed to close namespace archive: %w", err)
	}

	stat, err := tmp.Stat()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to stat namespace archive: %w", err)
	}

	reader, err := zip.NewReader(tmp, stat.Size())
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read namespace archive: %w", err)
	}

	for _, f := range reader.File {
		hdr := f.FileHeader

		// A "." entry is quickzip's name for the chroot root, which only appears
		// when the target IS its anchor base (a bare "~" or "."). Name it after
		// the namespace; splitNamespace drops a namespace-only entry on extract,
		// so the base dir's own mode isn't restored — harmless, since home and
		// cwd always already exist. prefix ends in "/" (a dir entry to klauspost),
		// so trim it for a non-directory root.
		switch {
		case path.Clean(f.Name) != ".":
			hdr.Name = prefix + f.Name
		case hdr.Mode().IsDir():
			hdr.Name = prefix
		default:
			hdr.Name = strings.TrimSuffix(prefix, "/")
		}

		w, err := zw.CreateRaw(&hdr)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to create archive entry %q: %w", hdr.Name, err)
		}

		rc, err := f.OpenRaw()
		if err != nil {
			return 0, 0, fmt.Errorf("failed to open raw entry %q: %w", f.Name, err)
		}

		if _, err := io.Copy(w, rc); err != nil {
			return 0, 0, fmt.Errorf("failed to copy entry %q: %w", f.Name, err)
		}
	}

	return writtenBytes, writtenEntries, nil
}
