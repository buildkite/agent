package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gowebpki/jcs"
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

type PipelineInvariants struct {
	OrganizationSlug string
	PipelineSlug     string
	Repository       string
}

type BuildInvariants struct {
	Id     string
	Number string
	Branch string
	Tag    string
	Commit string
}

type TimeInvariants struct {
	Expiry time.Time
}

type Invariants struct {
	Pipeline PipelineInvariants
	Build    BuildInvariants
	Time     TimeInvariants
}

type CommandStepWithInvariants struct {
	CommandStep
	Invariants
}

// SignedFields returns the default fields for signing.
func (c *CommandStepWithInvariants) SignedFields() (map[string]any, error) {
	return map[string]any{
		"command":    c.Command,
		"env":        c.Env,
		"plugins":    c.Plugins,
		"matrix":     c.Matrix,
		"invariants": c.Invariants,
	}, nil
}

// ValuesForFields returns the contents of fields to sign.
func (c *CommandStepWithInvariants) ValuesForFields(fields []string) (map[string]any, error) {
	// Make a set of required fields. As fields is processed, mark them off by
	// deleting them.
	required := map[string]struct{}{
		"command":    {},
		"env":        {},
		"plugins":    {},
		"matrix":     {},
		"invariants": {},
	}

	out := make(map[string]any, len(fields))
	for _, f := range fields {
		delete(required, f)

		switch f {
		case "command":
			out["command"] = c.Command

		case "env":
			out["env"] = c.Env

		case "plugins":
			out["plugins"] = c.Plugins

		case "matrix":
			out["matrix"] = c.Matrix

		case "invariants":
			out["invariants"] = c.Invariants

		default:
			// All env:: values come from outside the step.
			if strings.HasPrefix(f, EnvNamespacePrefix) {
				break
			}

			return nil, fmt.Errorf("unknown or unsupported field for signing %q", f)
		}
	}

	if len(required) > 0 {
		missing := make([]string, 0, len(required))
		for k := range required {
			missing = append(missing, k)
		}
		return nil, fmt.Errorf("one or more required fields are not present: %v", missing)
	}
	return out, nil
}

// Sign computes a new signature for an environment (env) combined with an
// object containing values (sf) using a given key.
func Sign(
	env map[string]string,
	sf SignedFielder,
	key jwk.Key,
) (*Signature, error) {
	values, err := sf.SignedFields()
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, errors.New("no fields to sign")
	}

	// Step env overrides pipeline and build env:
	// https://buildkite.com/docs/tutorials/pipeline-upgrade#what-is-the-yaml-steps-editor-compatibility-issues
	// (Beware of inconsistent docs written in the time of legacy steps.)
	// So if the thing we're signing has an env map, use it to exclude pipeline
	// vars from signing.
	objEnv, _ := values["env"].(map[string]string)

	// Namespace the env values and include them in the values to sign.
	for k, v := range env {
		if _, has := objEnv[k]; has {
			continue
		}
		values[EnvNamespacePrefix+k] = v
	}

	// Extract the names of the fields.
	fields := make([]string, 0, len(values))
	for field := range values {
		fields = append(fields, field)
	}
	sort.Strings(fields)

	payload, err := canonicalPayload(key.Algorithm().String(), values)
	if err != nil {
		return nil, err
	}

	sig, err := jws.Sign(nil,
		jws.WithKey(key.Algorithm(), key),
		jws.WithDetachedPayload(payload),
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
func (s *Signature) Verify(
	env map[string]string,
	sf SignedFielder,
	keySet jwk.Set,
) error {
	if len(s.SignedFields) == 0 {
		return errors.New("signature covers no fields")
	}

	// Ask the object for values for all fields.
	values, err := sf.ValuesForFields(s.SignedFields)
	if err != nil {
		return fmt.Errorf("obtaining values for fields: %w", err)
	}

	// See Sign above for why we need special handling for step env.
	objEnv, _ := values["env"].(map[string]string)

	// Namespace the env values and include them in the values to sign.
	for k, v := range env {
		if _, has := objEnv[k]; has {
			continue
		}
		values[EnvNamespacePrefix+k] = v
	}

	// env:: fields that were signed are all required from the env map.
	// We can't verify other env vars though - they can vary for lots of reasons
	// (e.g. Buildkite-provided vars added by the backend.)
	// This is still strong enough for a user to enforce any particular env var
	// exists and has a particular value - make it a part of the pipeline or
	// step env.
	required, err := requireKeys(values, s.SignedFields)
	if err != nil {
		return fmt.Errorf("obtaining required keys: %w", err)
	}

	payload, err := canonicalPayload(s.Algorithm, required)
	if err != nil {
		return err
	}

	_, err = jws.Verify([]byte(s.Value),
		jws.WithKeySet(keySet),
		jws.WithDetachedPayload(payload),
	)
	return err
}

// canonicalPayload returns a unique sequence of bytes representing the given
// algorithm and values using JCS (RFC 8785).
func canonicalPayload(alg string, values map[string]any) ([]byte, error) {
	rawPayload, err := json.Marshal(struct {
		Algorithm string         `json:"alg"`
		Values    map[string]any `json:"values"`
	}{
		Algorithm: alg,
		Values:    values,
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling JSON: %w", err)
	}
	payload, err := jcs.Transform(rawPayload)
	if err != nil {
		return nil, fmt.Errorf("canonicalising JSON: %w", err)
	}
	return payload, nil
}

// SignedFielder describes types that can be signed and have signatures
// verified.
// Converting non-string fields into strings (in a stable, canonical way) is an
// exercise left to the implementer.
type SignedFielder interface {
	// SignedFields returns the default set of fields to sign, and their values.
	// This is called by Sign.
	SignedFields() (map[string]any, error)

	// ValuesForFields looks up each field and produces a map of values. This is
	// called by Verify. The set of fields might differ from the default, e.g.
	// when verifying older signatures computed with fewer fields or deprecated
	// field names. signedFielder implementations should reject requests for
	// values if "mandatory" fields are missing (e.g. signing a command step
	// should always sign the command).
	ValuesForFields([]string) (map[string]any, error)
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
