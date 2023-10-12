package pipeline

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/jwkutil"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

const keyID = "chartreuse" // chosen by fair dice roll (unimportant what the value actually is)

func TestSignVerify(t *testing.T) {
	step := &CommandStep{
		Command: "llamas",
		Plugins: Plugins{
			{
				Source: "some-plugin#v1.0.0",
				Config: nil,
			},
			{
				Source: "another-plugin#v3.4.5",
				Config: map[string]any{"llama": "Kuzco"},
			},
		},
		Env: map[string]string{
			"CONTEXT": "cats",
			"DEPLOY":  "0",
		},
	}
	// The pipeline-level env that the agent uploads:
	signEnv := map[string]string{
		"DEPLOY": "1",
	}
	// The backend combines the pipeline and step envs, providing a new env:
	verifyEnv := map[string]string{
		"CONTEXT": "cats",
		"DEPLOY":  "1", // NB: pipeline env overrides step env.
		"MISC":    "llama drama",
	}

	cases := []struct {
		name                           string
		generateSigner                 func(alg jwa.SignatureAlgorithm) (jwk.Set, jwk.Set, error)
		alg                            jwa.SignatureAlgorithm
		expectedDeterministicSignature string
	}{
		{
			name: "HMAC-SHA256",
			generateSigner: func(alg jwa.SignatureAlgorithm) (jwk.Set, jwk.Set, error) {
				return jwkutil.NewSymmetricKeyPairFromString(keyID, "alpacas", alg)
			},
			alg:                            jwa.HS256,
			expectedDeterministicSignature: "eyJhbGciOiJIUzI1NiIsImtpZCI6ImNoYXJ0cmV1c2UifQ..yZI860Srtl1d2T3VKgTUVugFF_SX4VLqH-vN8zTw5fs",
		},
		{
			name: "HMAC-SHA384",
			generateSigner: func(alg jwa.SignatureAlgorithm) (jwk.Set, jwk.Set, error) {
				return jwkutil.NewSymmetricKeyPairFromString(keyID, "alpacas", alg)
			},
			alg:                            jwa.HS384,
			expectedDeterministicSignature: "eyJhbGciOiJIUzM4NCIsImtpZCI6ImNoYXJ0cmV1c2UifQ..dvKFu43gBBy3nKz4JgtqJbo4z4mNUZkOuD9nIGlslrnmQkcznE9pzbd-GtBtAetF",
		},
		{
			name: "HMAC-SHA512",
			generateSigner: func(alg jwa.SignatureAlgorithm) (jwk.Set, jwk.Set, error) {
				return jwkutil.NewSymmetricKeyPairFromString(keyID, "alpacas", alg)
			},
			alg:                            jwa.HS512,
			expectedDeterministicSignature: "eyJhbGciOiJIUzUxMiIsImtpZCI6ImNoYXJ0cmV1c2UifQ..LciCRCDL_MgOph-1I6vYMEViwanJFdrF9Bh5eiUy9PKIc1RyGnHh7y07ydkwo5Fxf3GqQojhEtTlB8JWvGtaxg",
		},
		{
			name:           "RSA-PSS 256",
			generateSigner: func(alg jwa.SignatureAlgorithm) (jwk.Set, jwk.Set, error) { return jwkutil.NewKeyPair(keyID, alg) },
			alg:            jwa.PS256,
		},
		{
			name:           "RSA-PSS 384",
			generateSigner: func(alg jwa.SignatureAlgorithm) (jwk.Set, jwk.Set, error) { return jwkutil.NewKeyPair(keyID, alg) },
			alg:            jwa.PS384,
		},
		{
			name:           "RSA-PSS 512",
			generateSigner: func(alg jwa.SignatureAlgorithm) (jwk.Set, jwk.Set, error) { return jwkutil.NewKeyPair(keyID, alg) },
			alg:            jwa.PS512,
		},
		{
			name:           "ECDSA P-256",
			generateSigner: func(alg jwa.SignatureAlgorithm) (jwk.Set, jwk.Set, error) { return jwkutil.NewKeyPair(keyID, alg) },
			alg:            jwa.ES256,
		},
		{
			name:           "ECDSA P-384",
			generateSigner: func(alg jwa.SignatureAlgorithm) (jwk.Set, jwk.Set, error) { return jwkutil.NewKeyPair(keyID, alg) },
			alg:            jwa.ES384,
		},
		{
			name:           "ECDSA P-512",
			generateSigner: func(alg jwa.SignatureAlgorithm) (jwk.Set, jwk.Set, error) { return jwkutil.NewKeyPair(keyID, alg) },
			alg:            jwa.ES512,
		},
		{
			name:           "EdDSA Ed25519",
			generateSigner: func(alg jwa.SignatureAlgorithm) (jwk.Set, jwk.Set, error) { return jwkutil.NewKeyPair(keyID, alg) },
			alg:            jwa.EdDSA,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			signer, verifier, err := tc.generateSigner(tc.alg)
			if err != nil {
				t.Fatalf("generateSigner(%v) error = %v", tc.alg, err)
			}

			key, ok := signer.Key(0)
			if !ok {
				t.Fatalf("signer.Key(0) = _, false, want true")
			}

			sig, err := Sign(signEnv, step, key)
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

			if err := sig.Verify(verifyEnv, step, verifier); err != nil {
				t.Errorf("sig.Verify(CommandStep, verifier) = %v", err)
			}
		})
	}
}

type testFields map[string]any

func (m testFields) SignedFields() (map[string]any, error) { return m, nil }

func (m testFields) ValuesForFields(fields []string) (map[string]any, error) {
	out := make(map[string]any, len(fields))
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

	signer, _, err := jwkutil.NewSymmetricKeyPairFromString(keyID, "alpacas", jwa.HS256)
	if err != nil {
		t.Fatalf("NewSymmetricKeyPairFromString(alpacas) error = %v", err)
	}

	key, ok := signer.Key(0)
	if !ok {
		t.Fatalf("signer.Key(0) = _, false, want true")
	}

	for _, m := range maps {
		sig, err := Sign(nil, m, key)
		if err != nil {
			t.Fatalf("Sign(%v, pts) error = %v", m, err)
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
	signer, _, err := jwkutil.NewSymmetricKeyPairFromString(keyID, "alpacas", jwa.HS256)
	if err != nil {
		t.Fatalf("NewSymmetricKeyPairFromString(alpacas) error = %v", err)
	}

	key, ok := signer.Key(0)
	if !ok {
		t.Fatalf("signer.Key(0) = _, false, want true")
	}

	key.Set(jwk.AlgorithmKey, "rot13")

	if _, err := Sign(nil, &CommandStep{Command: "llamas"}, key); err == nil {
		t.Errorf("Sign(nil, CommandStep, signer) = %v, want non-nil error", err)
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

	_, verifier, err := jwkutil.NewSymmetricKeyPairFromString(keyID, "alpacas", jwa.HS256)
	if err != nil {
		t.Fatalf("NewSymmetricKeyPairFromString(alpacas) error = %v", err)
	}

	if err := sig.Verify(nil, cs, verifier); err == nil {
		t.Errorf("sig.Verify(CommandStep, alpacas) = %v, want non-nil error", err)
	}
}

func TestSignUnknownStep(t *testing.T) {
	steps := Steps{
		&UnknownStep{
			Contents: "secret third thing",
		},
	}

	signer, _, err := jwkutil.NewSymmetricKeyPairFromString(keyID, "alpacas", jwa.HS256)
	if err != nil {
		t.Fatalf("NewSymmetricKeyPairFromString(alpacas) error = %v", err)
	}

	key, ok := signer.Key(0)
	if !ok {
		t.Fatalf("signer.Key(0) = _, false, want true")
	}

	if err := steps.sign(nil, key); !errors.Is(err, errSigningRefusedUnknownStepType) {
		t.Errorf("steps.sign(signer) = %v, want %v", err, errSigningRefusedUnknownStepType)
	}
}

func TestSignVerifyEnv(t *testing.T) {
	cases := []struct {
		name        string
		step        *CommandStep
		pipelineEnv map[string]string
		verifyEnv   map[string]string
	}{
		{
			name: "step env only",
			step: &CommandStep{
				Command: "llamas",
				Env: map[string]string{
					"CONTEXT": "cats",
					"DEPLOY":  "0",
				},
			},
			verifyEnv: map[string]string{
				"CONTEXT": "cats",
				"DEPLOY":  "0",
				"MISC":    "apple",
			},
		},
		{
			name: "pipeline env only",
			step: &CommandStep{
				Command: "llamas",
			},
			pipelineEnv: map[string]string{
				"CONTEXT": "cats",
				"DEPLOY":  "0",
			},
			verifyEnv: map[string]string{
				"CONTEXT": "cats",
				"DEPLOY":  "0",
				"MISC":    "apple",
			},
		},
		{
			name: "step and pipeline env",
			step: &CommandStep{
				Command: "llamas",
				Env: map[string]string{
					"CONTEXT": "cats",
					"DEPLOY":  "0",
				},
			},
			pipelineEnv: map[string]string{
				"CONTEXT": "dogs",
				"DEPLOY":  "1",
			},
			verifyEnv: map[string]string{
				// NB: pipeline env overrides step env.
				"CONTEXT": "dogs",
				"DEPLOY":  "1",
				"MISC":    "apple",
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			signer, verifier, err := jwkutil.NewSymmetricKeyPairFromString(keyID, "alpacas", jwa.HS256)
			if err != nil {
				t.Fatalf("NewSymmetricKeyPairFromString(alpacas) error = %v", err)
			}

			key, ok := signer.Key(0)
			if !ok {
				t.Fatalf("signer.Key(0) = _, false, want true")
			}

			sig, err := Sign(tc.pipelineEnv, tc.step, key)
			if err != nil {
				t.Fatalf("Sign(CommandStep, signer) error = %v", err)
			}

			if err := sig.Verify(tc.verifyEnv, tc.step, verifier); err != nil {
				t.Errorf("sig.Verify(CommandStep, verifier) = %v", err)
			}
		})
	}
}

func TestSignatureStability(t *testing.T) {
	t.Parallel()

	// The idea here is to sign and verify a step that is likely to encode in a
	// non-stable way if there are ordering bugs.

	pluginSubCfg := make(map[string]any)
	pluginCfg := map[string]any{
		"subcfg": pluginSubCfg,
	}
	step := &CommandStep{
		Command: "echo 'hello friend'",
		Env:     make(map[string]string),
		Plugins: Plugins{&Plugin{
			Source: "huge-config#v1.0.0",
			Config: pluginCfg,
		}},
	}
	env := make(map[string]string)

	// there are n! permutations of n items, but only one is correct
	// 128! is absurdly large, and we fill four maps...
	for i := 0; i < 128; i++ {
		env[fmt.Sprintf("VAR%08x", rand.Uint32())] = fmt.Sprintf("VAL%08x", rand.Uint32())
		step.Env[fmt.Sprintf("VAR%08x", rand.Uint32())] = fmt.Sprintf("VAL%08x", rand.Uint32())
		pluginCfg[fmt.Sprintf("key%08x", rand.Uint32())] = fmt.Sprintf("value%08x", rand.Uint32())
		pluginSubCfg[fmt.Sprintf("key%08x", rand.Uint32())] = fmt.Sprintf("value%08x", rand.Uint32())
	}

	signer, verifier, err := jwkutil.NewKeyPair(keyID, jwa.ES256)
	if err != nil {
		t.Fatalf("NewKeyPair error = %v", err)
	}

	key, ok := signer.Key(0)
	if !ok {
		t.Fatalf("signer.Key(0) = _, false, want true")
	}

	sig, err := Sign(env, step, key)
	if err != nil {
		t.Fatalf("Sign(env, CommandStep, signer) error = %v", err)
	}

	if err := sig.Verify(env, step, verifier); err != nil {
		t.Errorf("sig.Verify(env, CommandStep, verifier) = %v", err)
	}
}
