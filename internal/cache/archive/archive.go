package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	modifiedEpoch = "2024-01-01T00:00:00Z"
	bufferSize    = 1024 * 1024 * 20
	skipOwnership = true
)

type ArchiveInfo struct {
	ArchivePath    string
	Sha256sum      string
	Size           int64
	WrittenBytes   int64
	WrittenEntries int64
	Duration       time.Duration
}

// isUnderHome checks if the given path is under the user's home directory.
// It first gets the absolute path of the given path, then gets the user's
// home directory, and finally checks if the absolute path starts with the
// home directory path.
func isUnderHome(path string) (bool, error) {
	if path == "" {
		return false, fmt.Errorf("path is empty")
	}

	// Get absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Get user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false, fmt.Errorf("failed to get home directory: %w", err)
	}

	// Clean both paths to normalize them
	cleanPath := filepath.Clean(absPath)
	cleanHome := filepath.Clean(homeDir)

	// Check if the path starts with home directory
	return strings.HasPrefix(cleanPath, cleanHome), nil
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
