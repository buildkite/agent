package jwks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/buildkite/go-pipeline/jwkutil"
	"github.com/lestrrat-go/jwx/v3/jwa"
)

func TestKeyFromBytes(t *testing.T) {
	t.Parallel()

	privSet, _, err := jwkutil.NewKeyPair("test-key", jwa.EdDSA())
	if err != nil {
		t.Fatalf("jwkutil.NewKeyPair() error = %v", err)
	}

	data, err := json.Marshal(privSet)
	if err != nil {
		t.Fatalf("json.Marshal(privSet) error = %v", err)
	}

	t.Run("singleton without key ID", func(t *testing.T) {
		t.Parallel()

		key, err := KeyFromBytes(data, "")
		if err != nil {
			t.Fatalf("KeyFromBytes() error = %v", err)
		}

		kid, _ := key.KeyID()
		if kid != "test-key" {
			t.Errorf("key.KeyID() = %q, want %q", kid, "test-key")
		}
	})

	t.Run("matches jwkutil.LoadKey for the same data", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		path := filepath.Join(dir, "jwks.json")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("os.WriteFile() error = %v", err)
		}

		wantKey, err := jwkutil.LoadKey(path, "")
		if err != nil {
			t.Fatalf("jwkutil.LoadKey() error = %v", err)
		}

		gotKey, err := KeyFromBytes(data, "")
		if err != nil {
			t.Fatalf("KeyFromBytes() error = %v", err)
		}

		wantJSON, err := json.Marshal(wantKey)
		if err != nil {
			t.Fatalf("json.Marshal(wantKey) error = %v", err)
		}
		gotJSON, err := json.Marshal(gotKey)
		if err != nil {
			t.Fatalf("json.Marshal(gotKey) error = %v", err)
		}

		if string(wantJSON) != string(gotJSON) {
			t.Errorf("KeyFromBytes() = %s, want %s", gotJSON, wantJSON)
		}
	})

	t.Run("by key ID", func(t *testing.T) {
		t.Parallel()

		key, err := KeyFromBytes(data, "test-key")
		if err != nil {
			t.Fatalf("KeyFromBytes() error = %v", err)
		}

		kid, _ := key.KeyID()
		if kid != "test-key" {
			t.Errorf("key.KeyID() = %q, want %q", kid, "test-key")
		}
	})

	t.Run("unknown key ID", func(t *testing.T) {
		t.Parallel()

		_, err := KeyFromBytes(data, "does-not-exist")
		if !errors.Is(err, jwkutil.ErrCouldNotFindKeyByID) {
			t.Errorf("KeyFromBytes() error = %v, want %v", err, jwkutil.ErrCouldNotFindKeyByID)
		}
	})

	t.Run("invalid JWKS data", func(t *testing.T) {
		t.Parallel()

		_, err := KeyFromBytes([]byte("not a jwks"), "")
		if err == nil {
			t.Fatal("KeyFromBytes() error = nil, want an error")
		}
	})
}
