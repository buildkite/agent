package pipeline

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
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
	signedFields(version string) ([]string, error)
}

// Sign computes a signature. The signature covers the version (to prevent
// downgrades) and all the fields returned by the object.
func Sign(sf signedFielder, version string, key []byte) (*Signature, error) {
	fields, err := sf.signedFields(version)
	if err != nil {
		return nil, fmt.Errorf("signed fields: %w", err)
	}

	hash := hmac.New(sha256.New, key)

	encode(hash, version)
	encode(hash, fields...)

	// All done!
	return &Signature{
		Version: version,
		Value:   base64.StdEncoding.EncodeToString(hash.Sum(nil)),
	}, nil
}

// encode writes length-prefixed strings to the writer. This avoids ambiguity
// that could arise via string concatenation.
func encode(dst io.Writer, values ...string) {
	for _, s := range values {
		binary.Write(dst, binary.LittleEndian, uint32(len(s)))
		dst.Write([]byte(s))
	}
}
