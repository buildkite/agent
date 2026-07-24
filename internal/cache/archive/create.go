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

	"github.com/buildkite/agent/v3/internal/cache/internal/trace"
	"github.com/klauspost/compress/zip"
	"github.com/klauspost/compress/zstd"
	"github.com/wolfeidau/quickzip"
	"go.opentelemetry.io/otel/attribute"
)

// BuildArchive builds a cache archive for the given target paths.
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

	// Wrap the file in an io.Writer which records the sha256sum of the archive.
	checksummer := NewChecksumSHA256(archiveFile)
	zw := zip.NewWriter(checksummer)

	mappings, err := PathsToMappings(paths)
	if err != nil {
		return nil, fmt.Errorf("failed to get mappings: %w", err)
	}

	home, err := homeDir()
	if err != nil {
		return nil, err
	}
	cwd, err := workingDir()
	if err != nil {
		return nil, err
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
		if _, err := os.Stat(mapping.ResolvedPath); err != nil {
			if os.IsNotExist(err) {
				slog.Warn("cache path does not exist, skipping", "path", mapping.Path, "resolved", mapping.ResolvedPath)
				continue
			}
			return nil, fmt.Errorf("failed to stat file: %w", err)
		}

		chroot, prefix, err := saveLayout(mapping, home, cwd)
		if err != nil {
			return nil, err
		}

		plans = append(plans, plan{mapping: mapping, chroot: chroot, prefix: prefix})
		manifest.Mappings[mapping.Namespace] = mapping.Anchor
	}

	if err := writeManifest(zw, manifest); err != nil {
		return nil, err
	}

	var writtenBytes, writtenEntries int64
	for _, p := range plans {
		b, e, err := archiveMapping(ctx, zw, p.mapping.ResolvedPath, p.chroot, p.prefix, modified)
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

// saveLayout computes the quickzip chroot and the archive entry prefix for a
// mapping. The prefix is prepended to each quickzip entry name so that the
// final entry is "_N/<path-relative-to-anchor>/...".
func saveLayout(m Mapping, home, cwd string) (chroot, prefix string, err error) {
	switch {
	case m.Anchor == AnchorHome:
		return home, m.Namespace + "/", nil
	case m.Anchor == AnchorCWD:
		return cwd, m.Namespace + "/", nil
	case isRootAnchor(m.Anchor):
		// Pinned absolute path. m.Anchor is the volume root ("/" or "C:\"),
		// which is what the entries are relative to — so a Windows drive is
		// retained. quickzip can't chroot at a bare root, so chroot at the
		// target itself; the chroot-root "." entry is renamed onto the
		// namespace in archiveMapping, which handles a file, a symlink, or a
		// (possibly empty) directory alike. Only the root itself is rejected.
		if m.ResolvedPath == m.Anchor {
			return "", "", fmt.Errorf("cannot archive the volume/filesystem root %q", m.ResolvedPath)
		}
		rel, err := filepath.Rel(m.Anchor, m.ResolvedPath)
		if err != nil {
			return "", "", fmt.Errorf("failed to compute root-relative path for %q: %w", m.ResolvedPath, err)
		}
		return m.ResolvedPath, m.Namespace + "/" + filepath.ToSlash(rel) + "/", nil
	default:
		return "", "", fmt.Errorf("unknown anchor %q", m.Anchor)
	}
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

		// The chroot root entry (quickzip names it "." or "./") is the target
		// path itself — for a root-anchored path the chroot *is* the target.
		// Map it onto the namespace rather than dropping it, so an empty
		// directory, a directory symlink, and the target's own mode/metadata
		// all survive a round trip. `prefix` ends in "/", which klauspost reads
		// as a directory entry, so trim it for a non-directory root (a symlink
		// carries its link target as content and would be rejected otherwise).
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
