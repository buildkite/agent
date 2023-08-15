package pipeline

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
)

// Signature models a signature (on a step, etc).
type Signature struct {
	Algorithm    string   `json:"algorithm" yaml:"algorithm"`
	SignedFields []string `json:"signed_fields" yaml:"signed_fields"`
	Value        string   `json:"value" yaml:"value"`
}

// Sign computes a new signature for an object containing values using a given
// key.
func Sign(sf SignedFielder, key jwk.Key) (*Signature, error) {
	payload := &bytes.Buffer{}

	values, err := sf.SignedFields()
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, errors.New("sign: no fields to sign")
	}

	// Ensure this part is the same as in Verify...
	writeLengthPrefixed(payload, key.Algorithm().String())
	fields, err := writeFields(payload, values)
	if err != nil {
		return nil, err
	}
	// ...end

	sig, err := jws.Sign(nil,
		jws.WithKey(key.Algorithm(), key),
		jws.WithDetachedPayload(payload.Bytes()),
		jws.WithCompact(),
	)
	if err != nil {
		return nil, err
	}

	return &Signature{
		Algorithm:    key.Algorithm().String(),
		SignedFields: fields,
		Value:        string(sig),
	}, nil
}

// Verify verifies an existing signature against an object containing values
// using the verifier. Verify resets the verifier after use.
// (Verify does not create a new verifier based on the Algorithm field, in case
// you want to use a non-standard algorithm, but it must match the verifier's
// AlgorithmName).
func (s *Signature) Verify(sf SignedFielder, keySet jwk.Set) error {
	if len(s.SignedFields) == 0 {
		return errors.New("signature covers no fields")
	}

	values, err := sf.ValuesForFields(s.SignedFields)
	if err != nil {
		return fmt.Errorf("obtaining values for fields: %w", err)
	}

	payload := &bytes.Buffer{}
	writeLengthPrefixed(payload, s.Algorithm)
	if _, err := writeFields(payload, values); err != nil {
		return err
	}
	// ...end

	_, err = jws.Verify([]byte(s.Value),
		jws.WithKeySet(keySet),
		jws.WithDetachedPayload(payload.Bytes()),
	)
	return err
}

// SignedFielder describes types that can be signed and have signatures
// verified.
// Converting non-string fields into strings (in a stable, canonical way) is an
// exercise left to the implementer.
type SignedFielder interface {
	// SignedFields returns the default set of fields to sign, and their values.
	// This is called by Sign.
	SignedFields() (map[string]string, error)

	// ValuesForFields looks up each field and produces a map of values. This is
	// called by Verify. The set of fields might differ from the default, e.g.
	// when verifying older signatures computed with fewer fields or deprecated
	// field names. signedFielder implementations should reject requests for
	// values if "mandatory" fields are missing (e.g. signing a command step
	// should always sign the command).
	ValuesForFields([]string) (map[string]string, error)
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
