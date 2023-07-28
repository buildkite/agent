package pipeline

// import (
// 	"bytes"
// 	"errors"
// 	"fmt"
// 	"hash"
// 	"testing"

// 	"github.com/google/go-cmp/cmp"
// )

// func TestSignVerify(t *testing.T) {
// 	cs := &CommandStep{
// 		Command: "llamas",
// 	}

// 	const key = "alpacas"
// 	signer, err := NewSigner("hmac-sha256", key)
// 	if err != nil {
// 		t.Errorf("NewSigner(hmac-sha256, alpacas) error = %v", err)
// 	}
// 	sig, err := Sign(cs, signer)
// 	if err != nil {
// 		t.Errorf("Sign(CommandStep, signer) error = %v", err)
// 	}

// 	want := &Signature{
// 		Algorithm:    "hmac-sha256",
// 		SignedFields: []string{"command"},
// 		Value:        "GMetJcA3JqBNVDwljHRGfuW3t3Raixu+4+4dRYa0Cg0=",
// 	}
// 	if diff := cmp.Diff(sig, want); diff != "" {
// 		t.Errorf("Signature diff (-got +want):\n%s", diff)
// 	}

// 	verifier, err := NewVerifier("hmac-sha256", key)
// 	if err != nil {
// 		t.Errorf("NewVerifier(hmac-sha256, alpacas) error = %v", err)
// 	}
// 	if err := sig.Verify(cs, verifier); err != nil {
// 		t.Errorf("sig.Verify(CommandStep, verifier) = %v", err)
// 	}
// }

// type testFields map[string]string

// func (m testFields) SignedFields() map[string]string { return m }

// func (m testFields) ValuesForFields(fields []string) (map[string]string, error) {
// 	out := make(map[string]string, len(fields))
// 	for _, f := range fields {
// 		v, ok := m[f]
// 		if !ok {
// 			return nil, fmt.Errorf("unknown field %q", f)
// 		}
// 		out[f] = v
// 	}
// 	return out, nil
// }

// // Do not use this type outside of tests!
// type plainTextSigner struct {
// 	*bytes.Buffer
// 	hash.Hash
// }

// func (plainTextSigner) AlgorithmName() string { return "plaintext" }

// func (p plainTextSigner) Sign() ([]byte, error) {
// 	return p.Buffer.Bytes(), nil
// }

// func (p plainTextSigner) Write(b []byte) (int, error) {
// 	return p.Buffer.Write(b)
// }

// func (p plainTextSigner) Reset() { p.Buffer.Reset() }

// func TestSignConcatenatedFields(t *testing.T) {
// 	// Tests that Sign is resilient to concatenation.
// 	// Specifically, these maps should all have distinct "content". (If you
// 	// simply wrote the strings one after the other, they could be equal.)

// 	maps := []testFields{
// 		{
// 			"foo": "bar",
// 			"qux": "zap",
// 		},
// 		{
// 			"foob": "ar",
// 			"qu":   "xzap",
// 		},
// 		{
// 			"foo": "barquxzap",
// 		},
// 		{
// 			// Try really hard to fake matching content
// 			"foo": string([]byte{'b', 'a', 'r', 3, 0, 0, 0, 'q', 'u', 'x', 3, 0, 0, 0, 'z', 'a', 'p'}),
// 		},
// 	}

// 	sigs := make(map[string][]testFields)

// 	for _, m := range maps {
// 		pts := plainTextSigner{Buffer: new(bytes.Buffer)}
// 		sig, err := Sign(m, pts)
// 		if err != nil {
// 			t.Errorf("Sign(%v, pts) error = %v", m, err)
// 		}

// 		sigs[sig.Value] = append(sigs[sig.Value], m)
// 	}

// 	if len(sigs) != len(maps) {
// 		t.Error("some of the maps signed to the same value:")
// 		for _, ms := range sigs {
// 			if len(ms) == 1 {
// 				continue
// 			}
// 			t.Logf("had same signature: %v", ms)
// 		}
// 	}
// }

// func TestUnknownAlgorithm(t *testing.T) {
// 	if _, err := NewSigner("rot13", "alpacas"); err == nil {
// 		t.Errorf("NewSigner(rot13, alpacas) error = %v, want non-nil error", err)
// 	}
// }

// func TestVerifyBadSignature(t *testing.T) {
// 	cs := &CommandStep{
// 		Command: "llamas",
// 	}

// 	sig := &Signature{
// 		Algorithm:    "hmac-sha256",
// 		SignedFields: []string{"command"},
// 		Value:        "YWxwYWNhcw==", // base64("alpacas")
// 	}

// 	verifier, err := NewVerifier("hmac-sha256", "alpacas")
// 	if err != nil {
// 		t.Errorf("NewVerifier(hmac-sha256, alpacas) error = %v", err)
// 	}

// 	if err := sig.Verify(cs, verifier); err == nil {
// 		t.Errorf("sig.Verify(CommandStep, alpacas) = %v, want non-nil error", err)
// 	}
// }

// func TestSignUnknownStep(t *testing.T) {
// 	steps := Steps{
// 		&UnknownStep{
// 			Contents: "secret third thing",
// 		},
// 	}

// 	const key = "alpacas"
// 	signer, err := NewSigner("hmac-sha256", key)
// 	if err != nil {
// 		t.Errorf("NewSigner(hmac-sha256, alpacas) error = %v", err)
// 	}

// 	if err := steps.sign(signer); !errors.Is(err, errSigningRefusedUnknownStepType) {
// 		t.Errorf("steps.sign(signer) = %v, want %v", err, errSigningRefusedUnknownStepType)
// 	}
// }
