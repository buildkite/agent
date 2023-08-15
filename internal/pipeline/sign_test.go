package pipeline

import (
	"crypto/elliptic"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

func TestSignVerify(t *testing.T) {
	step := &CommandStep{
		Command: "llamas",
		Plugins: Plugins{
			{
				Name:   "some-plugin#v1.0.0",
				Config: nil,
			},
			{
				Name: "another-plugin#v3.4.5",
				Config: ordered.MapFromItems(
					ordered.TupleSA{
						Key:   "llama",
						Value: "Kuzco",
					},
				),
			},
		},
	}

	cases := []struct {
		name                           string
		generateSigner                 func(alg jwa.SignatureAlgorithm) (jwk.Key, jwk.Set)
		alg                            jwa.SignatureAlgorithm
		expectedDeterministicSignature string
	}{
		{
			name:                           "HMAC-SHA256",
			generateSigner:                 func(alg jwa.SignatureAlgorithm) (jwk.Key, jwk.Set) { return newSymmetricKeyPair(t, "alpacas", alg) },
			alg:                            jwa.HS256,
			expectedDeterministicSignature: "eyJhbGciOiJIUzI1NiIsImtpZCI6IlRlc3RTaWduVmVyaWZ5In0..NDiUjV0myH279-OQi6eOKjgyhAPUnc5ZmZoynhUUvIo",
		},
		{
			name:                           "HMAC-SHA384",
			generateSigner:                 func(alg jwa.SignatureAlgorithm) (jwk.Key, jwk.Set) { return newSymmetricKeyPair(t, "alpacas", alg) },
			alg:                            jwa.HS384,
			expectedDeterministicSignature: "eyJhbGciOiJIUzM4NCIsImtpZCI6IlRlc3RTaWduVmVyaWZ5In0..XGdZ7TG0lBSg7rXc091A3OaXAjODyI7aFkAjFJblD0YUnC5WW6WHgmJqlrG94x7z",
		},
		{
			name:                           "HMAC-SHA512",
			generateSigner:                 func(alg jwa.SignatureAlgorithm) (jwk.Key, jwk.Set) { return newSymmetricKeyPair(t, "alpacas", alg) },
			alg:                            jwa.HS512,
			expectedDeterministicSignature: "eyJhbGciOiJIUzUxMiIsImtpZCI6IlRlc3RTaWduVmVyaWZ5In0..GvKR_cGqNcF8EgffnkSoymJORoH60W36O80tYnGwnKXTUTh0XVmnEp0gT03YYRdf39JnwqbMGCticQJFFA_jWg",
		},
		{
			name:           "RSA-PSS 256",
			generateSigner: func(alg jwa.SignatureAlgorithm) (jwk.Key, jwk.Set) { return newRSAKeyPair(t, alg) },
			alg:            jwa.PS256,
		},
		{
			name:           "RSA-PSS 384",
			generateSigner: func(alg jwa.SignatureAlgorithm) (jwk.Key, jwk.Set) { return newRSAKeyPair(t, alg) },
			alg:            jwa.PS384,
		},
		{
			name:           "RSA-PSS 512",
			generateSigner: func(alg jwa.SignatureAlgorithm) (jwk.Key, jwk.Set) { return newRSAKeyPair(t, alg) },
			alg:            jwa.PS512,
		},
		{
			name:           "ECDSA P-256",
			generateSigner: func(alg jwa.SignatureAlgorithm) (jwk.Key, jwk.Set) { return newECKeyPair(t, alg, elliptic.P256()) },
			alg:            jwa.ES256,
		},
		{
			name:           "ECDSA P-384",
			generateSigner: func(alg jwa.SignatureAlgorithm) (jwk.Key, jwk.Set) { return newECKeyPair(t, alg, elliptic.P384()) },
			alg:            jwa.ES384,
		},
		{
			name:           "ECDSA P-512",
			generateSigner: func(alg jwa.SignatureAlgorithm) (jwk.Key, jwk.Set) { return newECKeyPair(t, alg, elliptic.P521()) },
			alg:            jwa.ES512,
		},
		{
			name:           "EdDSA Ed25519",
			generateSigner: func(alg jwa.SignatureAlgorithm) (jwk.Key, jwk.Set) { return newEdwardsKeyPair(t, alg) },
			alg:            jwa.EdDSA,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			signer, verifier := tc.generateSigner(tc.alg)

			sig, err := Sign(step, signer)
			if err != nil {
				t.Fatalf("Sign(CommandStep, signer) error = %v", err)
			}

			if sig.Algorithm != tc.alg.String() {
				t.Errorf("Signature.Algorithm = %v, want %v", sig.Algorithm, tc.alg)
			}

			if strings.HasPrefix(tc.alg.String(), "HS") {
				// Of all of the RFC7518 and RFC8037 JWA signing algorithms, only HMAC-SHA* (HS***) are deterministic
				// This means for all other algorithms, the signature value will be different each time, so we can't test
				// against it. We still verify that we can verify the signature, though.
				if sig.Value != tc.expectedDeterministicSignature {
					t.Errorf("Signature.Value = %v, want %v", sig.Value, tc.expectedDeterministicSignature)
				}
			}

			if err := sig.Verify(step, verifier); err != nil {
				t.Errorf("sig.Verify(CommandStep, verifier) = %v", err)
			}
		})
	}
}

type testFields map[string]string

func (m testFields) SignedFields() (map[string]string, error) { return m, nil }

func (m testFields) ValuesForFields(fields []string) (map[string]string, error) {
	out := make(map[string]string, len(fields))
	for _, f := range fields {
		v, ok := m[f]
		if !ok {
			return nil, fmt.Errorf("unknown field %q", f)
		}
		out[f] = v
	}
	return out, nil
}

func TestSignConcatenatedFields(t *testing.T) {
	// Tests that Sign is resilient to concatenation.
	// Specifically, these maps should all have distinct "content". (If you
	// simply wrote the strings one after the other, they could be equal.)

	maps := []testFields{
		{
			"foo": "bar",
			"qux": "zap",
		},
		{
			"foob": "ar",
			"qu":   "xzap",
		},
		{
			"foo": "barquxzap",
		},
		{
			// Try really hard to fake matching content
			"foo": string([]byte{'b', 'a', 'r', 3, 0, 0, 0, 'q', 'u', 'x', 3, 0, 0, 0, 'z', 'a', 'p'}),
		},
	}

	sigs := make(map[string][]testFields)

	signer, _ := newSymmetricKeyPair(t, "alpacas", jwa.HS256)
	for _, m := range maps {
		sig, err := Sign(m, signer)
		if err != nil {
			t.Errorf("Sign(%v, pts) error = %v", m, err)
		}

		sigs[sig.Value] = append(sigs[sig.Value], m)
	}

	if len(sigs) != len(maps) {
		t.Error("some of the maps signed to the same value:")
		for _, ms := range sigs {
			if len(ms) == 1 {
				continue
			}
			t.Logf("had same signature: %v", ms)
		}
	}
}

func TestUnknownAlgorithm(t *testing.T) {
	signer, _ := newSymmetricKeyPair(t, "alpacas", jwa.HS256)
	signer.Set(jwk.AlgorithmKey, "rot13")

	if _, err := Sign(&CommandStep{Command: "llamas"}, signer); err == nil {
		t.Errorf("Sign(CommandStep, signer) = %v, want non-nil error", err)
	}
}

func TestVerifyBadSignature(t *testing.T) {
	cs := &CommandStep{
		Command: "llamas",
	}

	sig := &Signature{
		Algorithm:    "HS256",
		SignedFields: []string{"command"},
		Value:        "YWxwYWNhcw==", // base64("alpacas")
	}

	_, verifier := newSymmetricKeyPair(t, "alpacas", jwa.HS256)
	if err := sig.Verify(cs, verifier); err == nil {
		t.Errorf("sig.Verify(CommandStep, alpacas) = %v, want non-nil error", err)
	}
}

func TestSignUnknownStep(t *testing.T) {
	steps := Steps{
		&UnknownStep{
			Contents: "secret third thing",
		},
	}

	signer, _ := newSymmetricKeyPair(t, "alpacas", jwa.HS256)
	if err := steps.sign(signer); !errors.Is(err, errSigningRefusedUnknownStepType) {
		t.Errorf("steps.sign(signer) = %v, want %v", err, errSigningRefusedUnknownStepType)
	}
}
