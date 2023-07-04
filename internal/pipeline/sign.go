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
	"math"
	"sort"
)

type newHash = func([]byte) hash.Hash

var algorithms = map[string]newHash{
	"hmac-sha256": func(secret []byte) hash.Hash {
		return hmac.New(sha256.New, secret)
	},
}

// Signature models a signature (on a step, etc).
type Signature struct {
	Algorithm    string   `json:"algorithm" yaml:"algorithm"`
	SignedFields []string `json:"signed_fields" yaml:"signed_fields"`
	Value        string   `json:"value" yaml:"value"`
}

// signedFielder describes types that can be signed and have signatures
// verified.
// Converting non-string fields into strings (in a stable, canonical way) is an
// exercise left to the implementer.
type signedFielder interface {
	// signedFields returns the default set of fields to sign, and their values.
	// This is called by Sign.
	signedFields() map[string]string

	// valuesForFields looks up each field and produces a map of values. This is
	// called by Verify. The set of fields might differ from the default, e.g.
	// when verifying older signatures computed with fewer fields or deprecated
	// field names. signedFielder implementations should reject requests for
	// values if "mandatory" fields are missing (e.g. signing a command step
	// should always sign the command).
	valuesForFields([]string) (map[string]string, error)
}

// Sign computes a new signature for a signedFielder.
func Sign(sf signedFielder, algName string, secret []byte) (*Signature, error) {
	newHash := algorithms[algName]
	if newHash == nil {
		return nil, fmt.Errorf("unknown signature algorithm %q", algName)
	}

	values := sf.signedFields()
	if len(values) == 0 {
		return nil, errors.New("sign: no fields to sign")
	}

	h := newHash(secret)
	writeLV(h, algName)
	fields, err := writeFields(h, values)
	if err != nil {
		return nil, err
	}
	return &Signature{
		Algorithm:    algName,
		SignedFields: fields,
		Value:        base64.StdEncoding.EncodeToString(h.Sum(nil)),
	}, nil
}

// Verify verifies an existing signature against an object containing values.
func (s *Signature) Verify(sf signedFielder, secret []byte) error {
	newHash := algorithms[s.Algorithm]
	if newHash == nil {
		return fmt.Errorf("unknown signature algorithm %q", s.Algorithm)
	}

	if len(s.SignedFields) == 0 {
		return errors.New("signature covers no fields")
	}

	sigval, err := base64.StdEncoding.DecodeString(s.Value)
	if err != nil {
		return fmt.Errorf("decoding signature value: %v", err)
	}

	values, err := sf.valuesForFields(s.SignedFields)
	if err != nil {
		return fmt.Errorf("obtaining values for fields: %w", err)
	}

	// Symmetric algorithm: compute the same hash, valid if they are equal.
	h := newHash(secret)
	writeLV(h, s.Algorithm)
	if _, err := writeFields(h, values); err != nil {
		return err
	}

	// These are hashes, so constant-time comparison is not needed.
	if computed := h.Sum(nil); !bytes.Equal(computed, sigval) {
		return fmt.Errorf("signature hash mismatch (signature is %x, but computed %x)", sigval, computed)
	}
	return nil
}

// writeFields writes the values (length-prefixed) into h. It also returns the
// sorted field names it got from values (so that Sign doesn't end up extracting
// them twice).
func writeFields(h hash.Hash, values map[string]string) (fields []string, err error) {
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
		writeLV(h, f)
		writeLV(h, values[f])
	}

	return fields, nil
}

// writeLV writes a length-prefixed string to h.
func writeLV(h hash.Hash, s string) {
	if len(s) > math.MaxUint32 {
		panic("input too large")
	}
	binary.Write(h, binary.LittleEndian, uint32(len(s)))
	h.Write([]byte(s))
}
