package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

	t.Run("unknown algorithm fails closed", func(t *testing.T) {
		// A digest we can't verify must not be treated as verified.
		err := verifyBlobDigest(t.Context(), archiveFile, api.CacheDigest{Algorithm: "blake3", Value: digestValue})
		if err == nil || errors.Is(err, ErrDigestMismatch) {
			t.Errorf("verifyBlobDigest with unknown algorithm = %v, want unsupported algorithm error", err)
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

func TestValidateBlobDigest(t *testing.T) {
	valid := strings.Repeat("ab", 32)
	if err := validateBlobDigest(api.CacheDigest{Algorithm: "sha256", Value: valid}); err != nil {
		t.Errorf("validateBlobDigest(valid sha256) = %v, want nil", err)
	}

	for _, digest := range []api.CacheDigest{
		{Algorithm: "blake3", Value: valid},
		{Algorithm: "", Value: valid},
		{Algorithm: "sha256", Value: ""},
		{Algorithm: "sha256", Value: strings.ToUpper(valid)},
		{Algorithm: "sha256", Value: valid[:63]},
		{Algorithm: "sha256", Value: valid + "a"},
		{Algorithm: "sha256", Value: "../" + valid[3:]},
		{Algorithm: "sha256", Value: valid[:62] + "/a"},
		{Algorithm: "sha256", Value: valid + "\n"},
	} {
		if err := validateBlobDigest(digest); err == nil {
			t.Errorf("validateBlobDigest(%+v) = nil, want error", digest)
		}
	}
}
