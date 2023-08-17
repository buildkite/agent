package pipeline

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
)

// EnvNamespacePrefix is the string that prefixes all fields in the "env"
// namespace. This is used to separate signed data that came from the
// environment from data that came from an object.
const EnvNamespacePrefix = "env::"

// Signature models a signature (on a step, etc).
type Signature struct {
	Algorithm    string   `json:"algorithm" yaml:"algorithm"`
	SignedFields []string `json:"signed_fields" yaml:"signed_fields"`
	Value        string   `json:"value" yaml:"value"`
}

// Sign computes a new signature for an environment (env) combined with an
// object containing values (sf) using a given key.
func Sign(env map[string]string, sf SignedFielder, key jwk.Key) (*Signature, error) {
	values, err := sf.SignedFields()
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, errors.New("no fields to sign")
	}

	// Namespace the env values.
	env = prefixKeys(env, EnvNamespacePrefix)

	// NB: env overrides values from sf. This may seem backwards but it is
	// our documented behaviour:
	// https://buildkite.com/docs/pipelines/environment-variables#defining-your-own
	// This override is handled by mapUnion.

	// Ensure this part writes the same data to the payload as in Verify...
	payload := &bytes.Buffer{}
	writeLengthPrefixed(payload, key.Algorithm().String())
	fields := writeFields(payload, mapUnion(values, env))
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

// Verify verifies an existing signature against environment (env) combined with
// an object containing values (sf) using keys from a keySet.
func (s *Signature) Verify(env map[string]string, sf SignedFielder, keySet jwk.Set) error {
	if len(s.SignedFields) == 0 {
		return errors.New("signature covers no fields")
	}

	// Ask the object for all fields, including env:: (which may be overridden
	// by the pipeline env).
	values, err := sf.ValuesForFields(s.SignedFields)
	if err != nil {
		return fmt.Errorf("obtaining values for fields: %w", err)
	}

	// env:: fields that were signed are all required in the env map.
	// We can't verify other env vars though - they can vary for lots of reasons
	// (e.g. Buildkite-provided vars added by the backend.)
	// This is still strong enough for a user to enforce any particular env var
	// exists and has a particular value - make it a part of the pipeline or
	// step env.
	envVars := filterPrefix(s.SignedFields, EnvNamespacePrefix)
	env, err = requireKeys(prefixKeys(env, EnvNamespacePrefix), envVars)
	if err != nil {
		return fmt.Errorf("obtaining values for env vars: %w", err)
	}

	// Ensure this part writes the same data to the payload as in Sign...
	payload := &bytes.Buffer{}
	writeLengthPrefixed(payload, s.Algorithm)
	writeFields(payload, mapUnion(values, env))
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
// them twice). It assumes writes to h never error (this is true of
// bytes.Buffer, strings.Builder, and most hash.Hash implementations).
// Passing a nil or empty map results in length 0 and no items being written.
func writeFields(h io.Writer, values map[string]string) []string {
	// Extract the field names and sort them.
	fields := make([]string, 0, len(values))
	for f := range values {
		fields = append(fields, f)
	}
	sort.Strings(fields)

	// If we blast strings at Write, then you could get the same hash for
	// different fields that happen to have the same data when concatenated.
	// So write length-prefixed fields, and length-prefix the whole map.
	binary.Write(h, binary.LittleEndian, uint32(len(fields)))
	for _, f := range fields {
		writeLengthPrefixed(h, f)
		writeLengthPrefixed(h, values[f])
	}

	return fields
}

// writeLengthPrefixed writes a length-prefixed string to h. It assumes writes
// to h never error (this is true of bytes.Buffer, strings.Builder, and most
// hash.Hash implementations).
func writeLengthPrefixed(h io.Writer, s string) {
	binary.Write(h, binary.LittleEndian, uint32(len(s)))
	h.Write([]byte(s))
}

// prefixKeys returns a copy of a map with each key prefixed with a prefix.
func prefixKeys[V any, M ~map[string]V](in M, prefix string) M {
	out := make(M, len(in))
	for k, v := range in {
		out[prefix+k] = v
	}
	return out
}

// filterPrefix returns values from the slice having the prefix.
func filterPrefix(in []string, prefix string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if strings.HasPrefix(s, prefix) {
			out = append(out, s)
		}
	}
	return out
}

// requireKeys returns a copy of a map containing only keys from a []string.
// An error is returned if any keys are missing from the map.
func requireKeys[K comparable, V any, M ~map[K]V](in M, keys []K) (M, error) {
	out := make(M, len(keys))
	for _, k := range keys {
		v, ok := in[k]
		if !ok {
			return nil, fmt.Errorf("missing key %v", k)
		}
		out[k] = v
	}
	return out, nil
}

// mapUnion returns a new map with all elements from the given maps.
// In case of key collisions, the last-most map containing the key "wins".
func mapUnion[K comparable, V any, M ~map[K]V](ms ...M) M {
	s := 0
	for _, m := range ms {
		s += len(m)
	}
	u := make(M, s)
	for _, m := range ms {
		for k, v := range m {
			u[k] = v
		}
	}
	return u
}
