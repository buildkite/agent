package reuse

import (
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/gowebpki/jcs"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
)

// SignatureField is the command step field holding the reuse signature, which
// covers the step's reuse declaration and is bound to its step signature.
const SignatureField = "reuse_signature"

var (
	// ErrNoDeclarationSignature means a step declares step reuse but has no
	// reuse signature.
	ErrNoDeclarationSignature = errors.New("the step declares step reuse but has no reuse_signature")

	// ErrDeclarationMismatch means a valid reuse signature doesn't cover the
	// step's current reuse fields or step signature.
	ErrDeclarationMismatch = errors.New("reuse_signature doesn't match the step's reuse fields")
)

// signedStepFields are the command step fields covered by the reuse signature,
// besides the step signature itself.
var signedStepFields = []string{"inputs", "outputs", "reuse", "inputs_digest"}

// HasDeclaration reports whether a command step's remaining fields declare
// step reuse, so that the step needs a reuse signature.
func HasDeclaration(remaining map[string]any) bool {
	for _, f := range []string{"inputs", "outputs", "reuse"} {
		if _, ok := remaining[f]; ok {
			return true
		}
	}
	return false
}

// Signature is a signature over a reuse declaration, stored in the step's
// `reuse_signature` field.
type Signature struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

// SigningKey is a key that can sign: a jwk.Key or a crypto.Signer that knows
// its algorithm, as used for step signatures.
type SigningKey interface {
	Algorithm() (jwa.KeyAlgorithm, bool)
}

// Canonical returns the JCS (RFC 8785) canonical JSON of v.
func Canonical(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshaling JSON: %w", err)
	}
	out, err := jcs.Transform(raw)
	if err != nil {
		return nil, fmt.Errorf("canonicalising JSON: %w", err)
	}
	return out, nil
}

// DeclarationPayload returns the canonical payload of a reuse signature: the
// step signature value and the step's reuse fields (null when absent).
func DeclarationPayload(stepSignature string, remaining map[string]any) ([]byte, error) {
	obj := map[string]any{"step_signature": stepSignature}
	for _, f := range signedStepFields {
		obj[f] = remaining[f]
	}
	return Canonical(obj)
}

// SignDeclaration signs the reuse declaration of a command step that has
// step signature stepSignature.
func SignDeclaration(key SigningKey, stepSignature string, remaining map[string]any) (*Signature, error) {
	payload, err := DeclarationPayload(stepSignature, remaining)
	if err != nil {
		return nil, err
	}
	alg, value, err := Sign(key, payload)
	if err != nil {
		return nil, err
	}
	return &Signature{Algorithm: alg, Value: value}, nil
}

// VerifyDeclaration verifies a command step's reuse signature against keySet,
// and that it covers the step's current reuse fields and step signature.
func VerifyDeclaration(keySet any, stepSignature string, remaining map[string]any) error {
	raw, ok := remaining[SignatureField]
	if !ok || raw == nil {
		return ErrNoDeclarationSignature
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("reading reuse_signature: %w", err)
	}
	var sig Signature
	if err := json.Unmarshal(b, &sig); err != nil || sig.Value == "" {
		return fmt.Errorf("reuse_signature is malformed: %v", cmpErr(err, "empty value"))
	}
	got, err := Verify(keySet, sig.Value)
	if err != nil {
		return fmt.Errorf("reuse_signature doesn't verify: %w", err)
	}
	want, err := DeclarationPayload(stepSignature, remaining)
	if err != nil {
		return err
	}
	if string(got) != string(want) {
		return ErrDeclarationMismatch
	}
	return nil
}

func cmpErr(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	return fallback
}

// Sign signs payload with key, returning the algorithm and a JWS in compact
// serialization with the payload embedded. A jwk.Key's key ID is set as the
// `kid` header, as for step signatures.
func Sign(key SigningKey, payload []byte) (alg, value string, err error) {
	a, ok := key.Algorithm()
	if !ok {
		return "", "", errors.New("the signing key has no algorithm")
	}
	sig, err := jws.Sign(payload, jws.WithKey(a, key), jws.WithCompact())
	if err != nil {
		return "", "", err
	}
	return a.String(), string(sig), nil
}

// Verify verifies a compact JWS against keySet, which is a jwk.Set or a
// crypto.Signer (as for step verification), and returns its payload.
func Verify(keySet any, value string) ([]byte, error) {
	var opt jws.VerifyOption
	switch ks := keySet.(type) {
	case jwk.Set:
		opt = jws.WithKeySet(ks)
	case crypto.Signer:
		alg := jwa.KeyAlgorithm(jwa.ES256())
		if k, ok := ks.(SigningKey); ok {
			if a, has := k.Algorithm(); has {
				alg = a
			}
		}
		opt = jws.WithKey(alg, ks)
	default:
		return nil, fmt.Errorf("unsupported verification key type %T", keySet)
	}
	return jws.Verify([]byte(value), opt)
}

// ManifestVersion is the version of result manifests.
const ManifestVersion = 1

// Manifest is a signed record of a job's stored result, which restoring jobs
// check before using its output paths.
type Manifest struct {
	Version    int               `json:"version"`
	JobID      string            `json:"job_id"`
	StepKey    string            `json:"step_key"`
	ExitStatus int               `json:"exit_status"`
	Paths      []ManifestPath    `json:"paths"`
	Values     map[string]string `json:"values"`
}

// ManifestPath is an output path in a Manifest.
type ManifestPath struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

// SignManifest signs m, sorting its paths, and returns the compact JWS.
func SignManifest(key SigningKey, m Manifest) (string, error) {
	m.Version = ManifestVersion
	m.Paths = slices.Clone(m.Paths)
	if m.Paths == nil {
		m.Paths = []ManifestPath{}
	}
	slices.SortFunc(m.Paths, func(a, b ManifestPath) int { return strings.Compare(a.Path, b.Path) })
	if m.Values == nil {
		m.Values = map[string]string{}
	}
	payload, err := Canonical(m)
	if err != nil {
		return "", err
	}
	_, value, err := Sign(key, payload)
	return value, err
}

// VerifyManifest verifies a signed manifest against keySet and returns it.
func VerifyManifest(keySet any, value string) (*Manifest, error) {
	payload, err := Verify(keySet, value)
	if err != nil {
		return nil, err
	}
	m := new(Manifest)
	if err := json.Unmarshal(payload, m); err != nil {
		return nil, fmt.Errorf("decoding the manifest: %w", err)
	}
	if m.Version != ManifestVersion {
		return nil, fmt.Errorf("unsupported manifest version %d", m.Version)
	}
	return m, nil
}

// Covers reports whether the manifest includes path with digest.
func (m *Manifest) Covers(path, digest string) bool {
	return slices.Contains(m.Paths, ManifestPath{Path: path, Digest: digest})
}
