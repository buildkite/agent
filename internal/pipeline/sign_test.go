package pipeline

import (
	"crypto/elliptic"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

func TestSignVerify(t *testing.T) {
	step := &CommandStep{
		Command: "llamas",
	}

	cases := []struct {
		name              string
		generateSigner    func(alg jwa.SignatureAlgorithm) (jwk.Key, jwk.Set)
		alg               jwa.SignatureAlgorithm
		expectedSignature string
	}{
		{
			name:              "HMAC-SHA256",
			generateSigner:    func(alg jwa.SignatureAlgorithm) (jwk.Key, jwk.Set) { return newSymmetricKeyPair(t, "alpacas", alg) },
			alg:               jwa.HS256,
			expectedSignature: "eyJhbGciOiJIUzI1NiIsImtpZCI6IlRlc3RTaWduVmVyaWZ5In0..f5NQYQtR0Eg-0pzzCon2ykzGy5oDPYtQw0C0fTKGI38",
		},
		{
			name:              "HMAC-SHA384",
			generateSigner:    func(alg jwa.SignatureAlgorithm) (jwk.Key, jwk.Set) { return newSymmetricKeyPair(t, "alpacas", alg) },
			alg:               jwa.HS384,
			expectedSignature: "eyJhbGciOiJIUzM4NCIsImtpZCI6IlRlc3RTaWduVmVyaWZ5In0..HgHltOlatth2TCc4swArP1UL_Zm2Rh2ccEC26s1sFBO6FOW5qfW37uQ9CHAz6dhh",
		},
		{
			name:              "HMAC-SHA512",
			generateSigner:    func(alg jwa.SignatureAlgorithm) (jwk.Key, jwk.Set) { return newSymmetricKeyPair(t, "alpacas", alg) },
			alg:               jwa.HS512,
			expectedSignature: "eyJhbGciOiJIUzUxMiIsImtpZCI6IlRlc3RTaWduVmVyaWZ5In0..mcph5zwioGkmx-aPrxExzc9QRzO4afn_kK_89aEuo4xYD0tcUD8OJom09x2xcvK6eRkOpvVlkrKLBzvh-7uu6w",
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
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			signer, verifier := tc.generateSigner(tc.alg)

			sig, err := Sign(step, signer)
			if err != nil {
				t.Errorf("Sign(CommandStep, signer) error = %v", err)
			}

			if sig.Algorithm != tc.alg.String() {
				t.Errorf("Signature.Algorithm = %v, want %v", sig.Algorithm, tc.alg)
			}

			if !strings.HasPrefix(tc.alg.String(), "PS") && !strings.HasPrefix(tc.alg.String(), "ES") {
				// It's impossible to generate deterministic RSA or ECDSA keys in golang (using the stdlib anyway - third party
				// libraries not withstanding), even with a seeded random source, so we can't test against the signature value
				// for asymmetric key algorithms. We can (and do) still check that we can verify the signature, though.
				// See: https://github.com/golang/go/issues/58637#issuecomment-1600627963
				if sig.Value != tc.expectedSignature {
					t.Errorf("Signature.Value = %v, want %v", sig.Value, tc.expectedSignature)
				}
			}

			if err := sig.Verify(step, verifier); err != nil {
				t.Errorf("sig.Verify(CommandStep, verifier) = %v", err)
			}
		})
	}
}

type testFields map[string]string

func (m testFields) SignedFields() map[string]string { return m }

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
