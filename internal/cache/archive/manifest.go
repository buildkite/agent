package archive

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zip"
)

const (
	// ManifestPath is the reserved archive entry.
	// Extraction reads it but never writes it to disk.
	ManifestPath = ".buildkite/cache-manifest.json"
	// ManifestVersion is the archive format version this agent reads and writes.
	ManifestVersion = 2
)

// ErrUnrecognizedFormat is returned when an archive has no readable manifest
var ErrUnrecognizedFormat = errors.New("unrecognized cache archive format")

// Manifest maps each namespace in an archive to its anchor.
type Manifest struct {
	Version  int               `json:"version"`
	Mappings map[string]string `json:"mappings"`
}

// writeManifest converts manifest into json, creates a
// new zip/archive file and writes the manifest to that file.
func writeManifest(zw *zip.Writer, manifest Manifest) error {
	data, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	w, err := zw.Create(ManifestPath)
	if err != nil {
		return fmt.Errorf("failed to create manifest entry: %w", err)
	}

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	return nil
}

// readManifest extracts and validates the manifest from an archive. It
// returns ErrUnrecognizedFormat when the manifest is absent or invalid.
func readManifest(reader *zip.Reader) (Manifest, error) {
	for _, f := range reader.File {
		if f.Name != ManifestPath {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return Manifest{}, fmt.Errorf("failed to open manifest: %w", err)
		}
		defer func() { _ = rc.Close() }()

		data, err := io.ReadAll(rc)
		if err != nil {
			return Manifest{}, fmt.Errorf("failed to read manifest: %w", err)
		}

		var manifest Manifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return Manifest{}, fmt.Errorf("%w: invalid manifest json: %w", ErrUnrecognizedFormat, err)
		}

		if manifest.Version != ManifestVersion {
			return Manifest{}, fmt.Errorf("%w: manifest version %d", ErrUnrecognizedFormat, manifest.Version)
		}

		for namespace, anchor := range manifest.Mappings {
			// The pinned-absolute anchor is a volume/filesystem root ("/" or a
			// Windows drive root), so accept any root, not just AnchorRoot.
			if anchor != AnchorHome && anchor != AnchorCWD && !isRootAnchor(anchor) {
				return Manifest{}, fmt.Errorf("%w: unrecognized anchor %q for namespace %q", ErrUnrecognizedFormat, anchor, namespace)
			}
		}
		return manifest, nil
	}
	return Manifest{}, fmt.Errorf("%w: no manifest entry", ErrUnrecognizedFormat)
}

// DetectFormat reports whether an archive is a readable archive. It returns
// ErrUnrecognizedFormat for unknown-version archives.
func DetectFormat(zipFile *os.File, zipFileLen int64) error {
	reader, err := zip.NewReader(zipFile, zipFileLen)
	if err != nil {
		return fmt.Errorf("failed to open zip reader: %w", err)
	}
	_, err = readManifest(reader)
	return err
}
