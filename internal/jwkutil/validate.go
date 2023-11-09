package jwkutil

import (
	"errors"
	"fmt"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"golang.org/x/exp/slices"
)

var (
	ValidRSAAlgorithms   = []jwa.SignatureAlgorithm{jwa.PS256, jwa.PS384, jwa.PS512}
	ValidECAlgorithms    = []jwa.SignatureAlgorithm{jwa.ES256, jwa.ES384, jwa.ES512}
	ValidOctetAlgorithms = []jwa.SignatureAlgorithm{jwa.HS256, jwa.HS384, jwa.HS512}
	ValidOKPAlgorithms   = []jwa.SignatureAlgorithm{jwa.EdDSA}

	ValidSigningAlgorithms = concat(
		ValidOctetAlgorithms,
		ValidRSAAlgorithms,
		ValidECAlgorithms,
		ValidOKPAlgorithms,
	)
)

var (
	ErrKeyMissingAlg             = errors.New("key is missing algorithm")
	ErrUnsupportedAlgForKeyType  = errors.New("unsupported key type")
	ErrInvalidSigningAlgorithm   = errors.New("invalid signing algorithm")
	ErrUnsupportSigningAlgorithm = errors.New("unsupported signing algorithm for key type")
)

func Validate(key jwk.Key) error {
	if err := key.Validate(); err != nil {
		return err
	}

	validKeyTypes := []jwa.KeyType{jwa.RSA, jwa.EC, jwa.OctetSeq, jwa.OKP}
	if !slices.Contains(validKeyTypes, key.KeyType()) {
		return fmt.Errorf(
			"%w: %q. Key type must be one of %q",
			ErrUnsupportedAlgForKeyType,
			key.KeyType(),
			validKeyTypes,
		)
	}

	if _, ok := key.Get(jwk.AlgorithmKey); !ok {
		return ErrKeyMissingAlg
	}

	signingAlg, ok := key.Algorithm().(jwa.SignatureAlgorithm)
	if !ok {
		return fmt.Errorf("%w: %q", ErrInvalidSigningAlgorithm, key.Algorithm())
	}

	validAlgsForType := map[jwa.KeyType][]jwa.SignatureAlgorithm{
		// We don't suppport RSA-PKCS1v1.5 because it's arguably less secure than RSA-PSS
		jwa.RSA:      {jwa.PS256, jwa.PS384, jwa.PS512},
		jwa.EC:       {jwa.ES256, jwa.ES384, jwa.ES512},
		jwa.OctetSeq: {jwa.HS256, jwa.HS384, jwa.HS512},
		jwa.OKP:      {jwa.EdDSA},
	}

	if !slices.Contains(validAlgsForType[key.KeyType()], signingAlg) {
		return fmt.Errorf(
			"%w: alg: %q, key type: %q. Expected alg to be one of %q",
			ErrUnsupportedAlgForKeyType,
			signingAlg,
			key.KeyType(),
			validAlgsForType[key.KeyType()],
		)
	}

	return nil
}

func concat[T any](a ...[]T) []T {
	capacity := 0
	for _, s := range a {
		capacity += len(s)
	}

	result := make([]T, 0, capacity)
	for _, s := range a {
		result = append(result, s...)
	}
	return result
}
