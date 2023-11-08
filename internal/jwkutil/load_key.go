package jwkutil

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/lestrrat-go/jwx/v2/jwk"
)

var (
	ErrNoSigningKeyID = errors.New(
		"a signing key ID is required when using a JWKS that does not have exactly one signing key",
	)
	ErrNoFirstKey = errors.New(
		"could not retrieve first key from a JWKS that has exactly one signing key. Maybe the JWKS file is corrupt?",
	)
	ErrCouldNotFindKeyByID = errors.New("could not be found in JWKS")
)

// LoadKey parses a JSON Web Key Set from a file path and returns the JSON Web
// Key identified by `keyID`. If the `keyID` is empty and the JSON Web Key Set
// is a singleton, it returns the only key in the key set.
func LoadKey(path, keyID string) (jwk.Key, error) {
	jwksFile, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening JWKS file: %w", err)
	}
	defer jwksFile.Close()

	jwksBody, err := io.ReadAll(jwksFile)
	if err != nil {
		return nil, fmt.Errorf("reading JWKS file: %w", err)
	}

	jwks, err := jwk.Parse(jwksBody)
	if err != nil {
		return nil, fmt.Errorf("parsing JWKS file: %w", err)
	}

	key, keyId, err := fromIdOrOnlyKey(jwks, keyID)
	if err != nil {
		return nil, err
	}

	if err := Validate(key); err != nil {
		return nil, fmt.Errorf("signing key ID %q is invalid: %w", keyId, err)
	}

	return key, nil
}

// fromIdOrOnlyKey looks up the key by ID. If the `keyID` is empty and the JSON Web Key Set
// is a singleton, it returns the only key in the key set.
func fromIdOrOnlyKey(jwks jwk.Set, keyID string) (jwk.Key, string, error) {
	if keyID == "" {
		if jwks.Len() != 1 {
			return nil, "", ErrNoSigningKeyID
		}

		key, found := jwks.Key(0)
		if !found {
			return nil, "", ErrNoFirstKey
		}

		return key, key.KeyID(), nil
	}

	key, found := jwks.LookupKeyID(keyID)
	if !found {
		return nil, "", fmt.Errorf("signing key ID %q %w", keyID, ErrCouldNotFindKeyByID)
	}

	return key, keyID, nil
}
