package pipeline

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"sort"
)

// Signature models a signature (on a step, etc).
type Signature struct {
	Version string `json:"version" yaml:"version"`
	Value   string `json:"value" yaml:"value"`
}

// signedFielder describes types that have fields that can be signed.
// Sign always includes the version in the hashed data, so implementations don't
// need to include it.
// Converting non-string fields into strings (in a stable, canonical way) is an
// exercise left to the implementer.
type signedFielder interface {
	signedFields(version string) (map[string]string, error)
}

// Sign computes a signature. The signature covers the version (to prevent
// downgrades) and all the fields returned by the object.
func Sign(sf signedFielder, version string, key []byte) (*Signature, error) {
	fields, err := sf.signedFields(version)
	if err != nil {
		return nil, fmt.Errorf("signed fields: %w", err)
	}

	hash := hmac.New(sha256.New, key)

	// Include the version to prevent downgrades.
	hash.Write([]byte(version))

	// First ensure they are written in a stable (sorted) order.
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// If we blast strings at hash.Write, then you could get the same hash for
	// different fields that happen to have the same data when concatenated.
	// So write length-prefixed fields, and length-prefix the whole map.
	binary.Write(hash, binary.LittleEndian, uint32(len(fields)))
	for _, k := range keys {
		binary.Write(hash, binary.LittleEndian, uint32(len(k)))
		hash.Write([]byte(k))

		v := fields[k]
		binary.Write(hash, binary.LittleEndian, uint32(len(v)))
		hash.Write([]byte(v))
	}

	// All done!
	return &Signature{
		Version: version,
		Value:   base64.StdEncoding.EncodeToString(hash.Sum(nil)),
	}, nil
}
