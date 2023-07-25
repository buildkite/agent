package pipeline

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"sort"

	"github.com/buildkite/agent/v3/internal/ordered"
)

// Signature models a signature (on a step, etc).
type Signature struct {
	Algorithm    string   `json:"algorithm" yaml:"algorithm"`
	SignedFields []string `json:"signed_fields" yaml:"signed_fields"`
	Value        string   `json:"value" yaml:"value"`
}

// Sign computes a new signature for an object containing values using a given
// signer. Sign resets the signer after use.
func Sign(sf SignedFielder, signer Signer) (*Signature, error) {
	values := sf.SignedFields()
	if len(values) == 0 {
		return nil, errors.New("sign: no fields to sign")
	}

	// Ensure this part is the same as in Verify...
	defer signer.Reset()
	writeLengthPrefixed(signer, signer.AlgorithmName())
	fields, err := writeFields(signer, values)
	if err != nil {
		return nil, err
	}
	// ...end

	sig, err := signer.Sign()
	if err != nil {
		return nil, err
	}

	return &Signature{
		Algorithm:    signer.AlgorithmName(),
		SignedFields: fields,
		Value:        base64.StdEncoding.EncodeToString(sig),
	}, nil
}

// Verify verifies an existing signature against an object containing values
// using the verifier. Verify resets the verifier after use.
// (Verify does not create a new verifier based on the Algorithm field, in case
// you want to use a non-standard algorithm, but it must match the verifier's
// AlgorithmName).
func (s *Signature) Verify(sf SignedFielder, verifier Verifier) error {
	if s.Algorithm != verifier.AlgorithmName() {
		return fmt.Errorf("algorithm name mismatch (signature alg = %q, verifier alg = %q)", s.Algorithm, verifier.AlgorithmName())
	}

	if len(s.SignedFields) == 0 {
		return errors.New("signature covers no fields")
	}

	sig, err := base64.StdEncoding.DecodeString(s.Value)
	if err != nil {
		return fmt.Errorf("decoding signature value: %w", err)
	}

	values, err := sf.ValuesForFields(s.SignedFields)
	if err != nil {
		return fmt.Errorf("obtaining values for fields: %w", err)
	}

	// Ensure this part is the same as in Sign...
	defer verifier.Reset()
	writeLengthPrefixed(verifier, verifier.AlgorithmName())
	if _, err := writeFields(verifier, values); err != nil {
		return err
	}
	// ...end

	return verifier.Verify(sig)
}

// unmarshalAny unmarshals an *ordered.Map[string, any] into this Signature.
// Any other type is an error.
func (s *Signature) unmarshalAny(o any) error {
	m, ok := o.(*ordered.MapSA)
	if !ok {
		return fmt.Errorf("unmarshaling signature: got %T, want *ordered.Map[string, any]", o)
	}

	return m.Range(func(k string, v any) error {
		switch k {
		case "algorithm":
			a, ok := v.(string)
			if !ok {
				return fmt.Errorf("unmarshaling signature: algorithm has type %T, want string", v)
			}
			s.Algorithm = a

		case "signed_fields":
			os, ok := v.([]any)
			if !ok {
				return fmt.Errorf("unmarshaling signature: signed_fields has type %T, want []any", v)
			}
			for _, of := range os {
				f, ok := of.(string)
				if !ok {
					return fmt.Errorf("unmarshaling signature: item in signed_fields has type %T, want string", of)
				}
				s.SignedFields = append(s.SignedFields, f)
			}

		case "value":
			a, ok := v.(string)
			if !ok {
				return fmt.Errorf("unmarshaling signature: value has type %T, want string", v)
			}
			s.Value = a

		default:
			return fmt.Errorf("unmarshaling signature: unsupported key %q", k)
		}
		return nil
	})
}

// SignedFielder describes types that can be signed and have signatures
// verified.
// Converting non-string fields into strings (in a stable, canonical way) is an
// exercise left to the implementer.
type SignedFielder interface {
	// SignedFields returns the default set of fields to sign, and their values.
	// This is called by Sign.
	SignedFields() map[string]string

	// ValuesForFields looks up each field and produces a map of values. This is
	// called by Verify. The set of fields might differ from the default, e.g.
	// when verifying older signatures computed with fewer fields or deprecated
	// field names. signedFielder implementations should reject requests for
	// values if "mandatory" fields are missing (e.g. signing a command step
	// should always sign the command).
	ValuesForFields([]string) (map[string]string, error)
}

// NewSigner returns a new Signer for the given algorithm,
// provided with a signing/verification key.
func NewSigner(algorithm string, key any) (Signer, error) {
	switch algorithm {
	case "hmac-sha256":
		return newHMACSHA256(key)
	default:
		return nil, fmt.Errorf("unknown signing algorithm %q", algorithm)
	}
}

// NewSigner returns a new Verifier for the given algorithm,
// provided with a signing/verification key.
func NewVerifier(algorithm string, key any) (Verifier, error) {
	switch algorithm {
	case "hmac-sha256":
		return newHMACSHA256(key)
	default:
		return nil, fmt.Errorf("unknown signing algorithm %q", algorithm)
	}
}

// Signer describes operations that support the Sign func.
type Signer interface {
	// Data written here is hashed into a digest. The signature is (at least
	// nominally) computed from the digest.
	hash.Hash

	// AlgorithmName returns the name of the algorithm (which should match the
	// argument to NewSigner).
	AlgorithmName() string

	// Sign returns a signature for the data written so far.
	Sign() ([]byte, error)
}

// Verifier describes operations that support the Signature.Verify method.
type Verifier interface {
	// Data written here is hashed into a digest. The verifier must check (at
	// least nominally) that the digest matches the data, and the signature is
	// a valid signature for the digest.
	hash.Hash

	// AlgorithmName returns the name of the algorithm (which should match the
	// argument to NewVerifier).
	AlgorithmName() string

	// Verify checks a given signature is valid for the data written so far.
	Verify([]byte) error
}

type hmacSHA256 struct {
	hash.Hash
}

func newHMACSHA256(key any) (hmacSHA256, error) {
	var bkey []byte
	switch tkey := key.(type) {
	case []byte:
		bkey = tkey

	case string:
		bkey = []byte(tkey)

	default:
		return hmacSHA256{}, fmt.Errorf("wrong key type (got %T, want []byte or string)", key)
	}
	return hmacSHA256{
		Hash: hmac.New(sha256.New, bkey),
	}, nil
}

func (h hmacSHA256) AlgorithmName() string { return "hmac-sha256" }

func (h hmacSHA256) Sign() ([]byte, error) {
	return h.Hash.Sum(nil), nil
}

func (h hmacSHA256) Verify(sig []byte) error {
	c := h.Hash.Sum(nil)
	if !bytes.Equal(c, sig) {
		return errors.New("signature mismatch")
	}
	return nil
}

// writeFields writes the values (length-prefixed) into h. It also returns the
// sorted field names it got from values (so that Sign doesn't end up extracting
// them twice).
func writeFields(h io.Writer, values map[string]string) (fields []string, err error) {
	if len(values) == 0 {
		return nil, errors.New("writeFields: no values to sign")
	}

	// Extract the field names and sort them.
	fields = make([]string, 0, len(values))
	for f := range values {
		fields = append(fields, f)
	}
	sort.Strings(fields)

	// If we blast strings at hash.Write, then you could get the same hash for
	// different fields that happen to have the same data when concatenated.
	// So write length-prefixed fields, and length-prefix the whole map.
	binary.Write(h, binary.LittleEndian, uint32(len(fields)))
	for _, f := range fields {
		writeLengthPrefixed(h, f)
		writeLengthPrefixed(h, values[f])
	}

	return fields, nil
}

// writeLengthPrefixed writes a length-prefixed string to h.
func writeLengthPrefixed(h io.Writer, s string) {
	binary.Write(h, binary.LittleEndian, uint32(len(s)))
	h.Write([]byte(s))
}
