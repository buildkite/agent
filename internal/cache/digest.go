package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/buildkite/agent/v4/api"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// ErrDigestMismatch means a downloaded blob's bytes don't hash to the
// content-addressed name it was fetched under (e.g. a corrupt or half-written
// upload). Restore treats it as a clean miss and never unpacks the file.
var ErrDigestMismatch = errors.New("cache blob digest mismatch")

// verifyBlobDigest re-hashes the downloaded archive and confirms it matches
// the content-addressed name it was fetched under.
//
// This re-reads the file the download just wrote. We considered hashing the
// bytes in the same pass as the download to avoid the second read, but the
// blob stores don't expose a sequential byte stream: the S3 downloader writes
// parts concurrently and out of order via io.WriterAt, and the nsc store
// shells out to a CLI that writes the file itself. In practice the second
// read is cheap anyway; the file was written moments earlier, so it is
// served from the OS page cache, and the SHA-256 cost is the same wherever
// the bytes are hashed.
func verifyBlobDigest(ctx context.Context, archiveFile string, digest api.CacheDigest) error {
	tracer := otel.Tracer("github.com/buildkite/agent/v4/internal/cache")
	ctx, span := tracer.Start(ctx, "verifyBlobDigest")
	defer span.End()

	span.SetAttributes(attribute.String("cache.digest_algorithm", digest.Algorithm))

	if digest.Algorithm != "sha256" {
		span.SetStatus(codes.Ok, "unsupported digest algorithm, verification skipped")
		return nil
	}

	f, err := os.Open(archiveFile)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to open archive")
		return fmt.Errorf("failed to open archive for digest check: %w", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, &contextReader{ctx: ctx, r: f}); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to hash archive")
		return fmt.Errorf("failed to hash archive for digest check: %w", err)
	}

	if got := hex.EncodeToString(h.Sum(nil)); got != digest.Value {
		err := fmt.Errorf("%w: computed %s, expected %s", ErrDigestMismatch, got, digest.Value)
		span.RecordError(err)
		span.SetAttributes(attribute.Bool("cache.digest_mismatch", true))
		span.SetStatus(codes.Error, "digest mismatch")
		return err
	}

	span.SetStatus(codes.Ok, "digest verified")
	return nil
}

// contextReader wraps an io.Reader so a cancelled restore aborts between reads
// instead of hashing the whole archive.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
