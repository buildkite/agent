package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
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
