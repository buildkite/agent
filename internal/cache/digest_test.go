package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/buildkite/agent/v4/api"
)

func TestVerifyBlobDigest(t *testing.T) {
	dir := t.TempDir()
	archiveFile := filepath.Join(dir, "archive.zip")
	content := []byte("hello cache archive")
	if err := os.WriteFile(archiveFile, content, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	sum := sha256.Sum256(content)
	digestValue := hex.EncodeToString(sum[:])

	t.Run("matching sha256 passes", func(t *testing.T) {
		err := verifyBlobDigest(t.Context(), archiveFile, api.CacheDigest{Algorithm: "sha256", Value: digestValue})
		if err != nil {
			t.Errorf("verifyBlobDigest = %v, want nil", err)
		}
	})

	t.Run("mismatched sha256 is ErrDigestMismatch", func(t *testing.T) {
		err := verifyBlobDigest(t.Context(), archiveFile, api.CacheDigest{Algorithm: "sha256", Value: "deadbeef"})
		if !errors.Is(err, ErrDigestMismatch) {
			t.Errorf("verifyBlobDigest = %v, want ErrDigestMismatch", err)
		}
	})

	t.Run("unknown algorithm is skipped, not reported corrupt", func(t *testing.T) {
		// A non-sha256 digest we can't verify must pass through, not be flagged as
		// a mismatch — otherwise a future wire format breaks every restore.
		err := verifyBlobDigest(t.Context(), archiveFile, api.CacheDigest{Algorithm: "blake3", Value: "unverifiable"})
		if err != nil {
			t.Errorf("verifyBlobDigest with unknown algorithm = %v, want nil (skip)", err)
		}
	})

	t.Run("cancelled context aborts", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err := verifyBlobDigest(ctx, archiveFile, api.CacheDigest{Algorithm: "sha256", Value: digestValue})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("verifyBlobDigest with cancelled ctx = %v, want context.Canceled", err)
		}
	})
}
