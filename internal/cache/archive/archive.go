package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"time"

	"github.com/klauspost/compress/zip"
)

const (
	modifiedEpoch = "2024-01-01T00:00:00Z"
	bufferSize    = 1024 * 1024 * 20
	skipOwnership = true

	// ManifestPath is the reserved archive entry that records the v2 layout.
	// Extraction reads it but never writes it to disk.
	ManifestPath = ".buildkite/cache-manifest.json"
	// ManifestVersion is the archive format version this agent reads and writes.
	ManifestVersion = 2
)

// ErrUnrecognizedFormat is returned when an archive has no readable manifest
var ErrUnrecognizedFormat = errors.New("unrecognized cache archive format")

// Manifest maps each namespace in an archive to its anchor. Anchors are
// symbols ("~", "/", "."), never resolved paths, so restore can re-resolve them
// against the local environment.
type Manifest struct {
	Version  int               `json:"version"`
	Mappings map[string]string `json:"mappings"`
}

type ArchiveInfo struct {
	ArchivePath    string
	Sha256sum      string
	Size           int64
	WrittenBytes   int64
	WrittenEntries int64
	Duration       time.Duration
}

// writeManifest serialises the manifest into the reserved manifest entry of the
// archive being written.
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
// returns ErrUnrecognizedFormat when the manifest is absent or its version is
// not understood.
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
			// A pinned-absolute anchor is a volume/filesystem root, which is "/"
			// on POSIX but a drive root (e.g. "C:\") on Windows, so accept any
			// root rather than only the AnchorRoot constant.
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

type ChecksumSHA256 struct {
	f      io.Writer
	sha256 hash.Hash
}

func NewChecksumSHA256(f io.Writer) *ChecksumSHA256 {
	return &ChecksumSHA256{
		f:      f,
		sha256: sha256.New(),
	}
}

// implement the io.WriteCloser interface
func (c *ChecksumSHA256) Write(p []byte) (n int, err error) {
	n, err = c.f.Write(p)
	if err != nil {
		return n, err
	}
	c.sha256.Write(p)
	return n, nil
}

func (c *ChecksumSHA256) Sum() string {
	return hex.EncodeToString(c.sha256.Sum(nil))
}
