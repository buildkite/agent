// Package jwks provides helpers for loading a JSON Web Key from in-memory JWKS
// data, without requiring the data to be read from a file on disk.
package jwks

import (
	"fmt"

	"github.com/buildkite/go-pipeline/jwkutil"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

// KeyFromBytes parses a JSON Web Key Set from data and returns the JSON Web Key
// identified by keyID. If keyID is empty and the JSON Web Key Set is a singleton,
// it returns the only key in the key set. This mirrors jwkutil.LoadKey, but
// operates on in-memory data rather than a file path.
func KeyFromBytes(data []byte, keyID string) (jwk.Key, error) {
	jwks, err := jwk.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parsing JWKS: %w", err)
	}

	key, foundKeyID, err := fromIDOrOnlyKey(jwks, keyID)
	if err != nil {
		return nil, err
	}

	if err := jwkutil.Validate(key); err != nil {
		return nil, fmt.Errorf("signing key ID %q is invalid: %w", foundKeyID, err)
	}

	return key, nil
}

func fromIDOrOnlyKey(jwks jwk.Set, keyID string) (jwk.Key, string, error) {
	if keyID == "" {
		if jwks.Len() != 1 {
			return nil, "", jwkutil.ErrNoSigningKeyID
		}

		key, found := jwks.Key(0)
		if !found {
			return nil, "", jwkutil.ErrNoFirstKey
		}
		id, _ := key.KeyID()
		return key, id, nil
	}

	key, found := jwks.LookupKeyID(keyID)
	if !found {
		return nil, "", fmt.Errorf("signing key ID %q %w", keyID, jwkutil.ErrCouldNotFindKeyByID)
	}

	return key, keyID, nil
}
