package pipeline

import (
	"bytes"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"sort"
)

type newSignatureScheme = func([]byte) (signatureScheme, error)

var algorithms = map[string]newSignatureScheme{
	"hmac-sha256":    newHMACSHA256,
	"rsa-pss-sha256": newRSAPSSSHA256,
}

type signatureScheme interface {
	io.Writer

	// Sign returns a signature for the data written so far.
	Sign() ([]byte, error)

	// Verify checks a given signature is valid for the data written so far.
	Verify([]byte) error
}

type hmacSHA256 struct {
	hash hash.Hash
}

func newHMACSHA256(key []byte) (signatureScheme, error) {
	return hmacSHA256{
		hash: hmac.New(sha256.New, key),
	}, nil
}

func (h hmacSHA256) Write(b []byte) (int, error) {
	return h.hash.Write(b)
}

func (h hmacSHA256) Sign() ([]byte, error) {
	return h.hash.Sum(nil), nil
}

func (h hmacSHA256) Verify(sig []byte) error {
	c := h.hash.Sum(nil)
	if !bytes.Equal(c, sig) {
		return fmt.Errorf("message digest mismatch (%x != %x)", c, sig)
	}
	return nil
}

type rsaPSSSHA256 struct {
	hash       hash.Hash
	publicKey  *rsa.PublicKey
	privateKey *rsa.PrivateKey
}

func newRSAPSSSHA256(key []byte) (signatureScheme, error) {
	switch {
	case bytes.Contains(key, []byte("RSA PRIVATE KEY")):
		privateKey, err := x509.ParsePKCS1PrivateKey(key)
		if err != nil {
			return nil, err
		}
		return &rsaPSSSHA256{
			hash:       sha256.New(),
			privateKey: privateKey,
			publicKey:  &privateKey.PublicKey,
		}, nil

	case bytes.Contains(key, []byte("RSA PUBLIC KEY")):
		publicKey, err := x509.ParsePKCS1PublicKey(key)
		if err != nil {
			return nil, err
		}
		return &rsaPSSSHA256{
			hash:      sha256.New(),
			publicKey: publicKey,
		}, nil

	default:
		return nil, errors.New("unknown key type")
	}
}

func (h *rsaPSSSHA256) Write(b []byte) (int, error) {
	return h.hash.Write(b)
}

func (h *rsaPSSSHA256) Sign() ([]byte, error) {
	if h.privateKey == nil {
		return nil, errors.New("cannot sign without private key")
	}
	digest := h.hash.Sum(nil)
	sig, err := rsa.SignPSS(rand.Reader, h.privateKey, crypto.SHA256, digest, nil)
	if err != nil {
		return nil, err
	}
	// We return signature value = digest + rsa sig
	// So we can detect data corruption separately from signing failures.
	return append(digest, sig...), nil
}

func (h *rsaPSSSHA256) Verify(sig []byte) error {
	if h.publicKey == nil {
		return errors.New("cannot verify without public key")
	}
	if len(sig) < h.hash.Size() {
		return fmt.Errorf("signature too small (%d bytes < %d)", len(sig), h.hash.Size())
	}
	// sig should start with digest
	digest := h.hash.Sum(nil)
	if !bytes.Equal(digest, sig[:len(digest)]) {
		return fmt.Errorf("message digest mismatch (%x != %x)", digest, sig[:len(digest)])
	}
	// The rest of sig should be the output from rsa.SignPSS.
	sig = sig[len(digest):]
	return rsa.VerifyPSS(h.publicKey, crypto.SHA256, digest, sig, nil)
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
func Sign(sf signedFielder, algName string, key []byte) (*Signature, error) {
	newss := algorithms[algName]
	if newss == nil {
		return nil, fmt.Errorf("unknown signature algorithm %q", algName)
	}

	values := sf.signedFields()
	if len(values) == 0 {
		return nil, errors.New("sign: no fields to sign")
	}

	// Ensure this part is the same as in Verify...
	h, err := newss(key)
	if err != nil {
		return nil, err
	}
	writeLV(h, algName)
	fields, err := writeFields(h, values)
	if err != nil {
		return nil, err
	}
	// ...end

	sig, err := h.Sign()
	if err != nil {
		return nil, err
	}
	return &Signature{
		Algorithm:    algName,
		SignedFields: fields,
		Value:        base64.StdEncoding.EncodeToString(sig),
	}, nil
}

// Verify verifies an existing signature against an object containing values.
func (s *Signature) Verify(sf signedFielder, key []byte) error {
	newss := algorithms[s.Algorithm]
	if newss == nil {
		return fmt.Errorf("unknown signature algorithm %q", s.Algorithm)
	}

	if len(s.SignedFields) == 0 {
		return errors.New("signature covers no fields")
	}

	sig, err := base64.StdEncoding.DecodeString(s.Value)
	if err != nil {
		return fmt.Errorf("decoding signature value: %v", err)
	}

	values, err := sf.valuesForFields(s.SignedFields)
	if err != nil {
		return fmt.Errorf("obtaining values for fields: %w", err)
	}

	// Ensure this part is the same as in Sign...
	h, err := newss(key)
	if err != nil {
		return err
	}
	writeLV(h, s.Algorithm)
	if _, err := writeFields(h, values); err != nil {
		return err
	}
	// ...end

	return h.Verify(sig)
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
		writeLV(h, f)
		writeLV(h, values[f])
	}

	return fields, nil
}

// writeLV writes a length-prefixed string to h.
func writeLV(h io.Writer, s string) {
	binary.Write(h, binary.LittleEndian, uint32(len(s)))
	h.Write([]byte(s))
}
