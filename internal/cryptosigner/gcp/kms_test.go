package gcpsigner

import (
	"crypto"
	"errors"
	"testing"

	"github.com/lestrrat-go/jwx/v2/jwa"
)

func TestNewKMS_InvalidKeyResourceName(t *testing.T) {
	ctx := t.Context()

	_, err := NewKMS(ctx, "")
	if err != ErrInvalidKeyResourceName {
		t.Errorf("NewKMS(ctx, \"\") error = %v, want %v", err, ErrInvalidKeyResourceName)
	}
}

func TestKMS_ComputeDigest_UnsupportedAlgorithm(t *testing.T) {
	k := &KMS{
		hashAlg: crypto.MD5, // Unsupported
	}

	_, err := k.ComputeDigest([]byte("test data"))
	if !errors.Is(err, ErrUnsupportedHashAlg) {
		t.Errorf("k.ComputeDigest([]byte(\"test data\")) error = %v, want %v", err, ErrUnsupportedHashAlg)
	}
}

func TestKMS_Close(t *testing.T) {
	k := &KMS{
		client: nil,
	}

	// Should not panic with nil client
	err := k.Close()
	if err != nil {
		t.Errorf("k.Close() error = %v, want nil", err)
	}
}

// TestKMS_Algorithm tests that the Algorithm method returns the correct JWA algorithm
func TestKMS_Algorithm(t *testing.T) {
	k := &KMS{
		jwaAlg: jwa.ES256,
	}

	if alg := k.Algorithm(); alg.String() != "ES256" {
		t.Errorf("Algorithm() = %v, want ES256", alg)
	}
}

// Example of how to mock GCP KMS client for integration tests
// This test is skipped by default as it requires a real GCP KMS key
func TestNewKMS_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	// This would require:
	// 1. A GCP project with KMS enabled
	// 2. A signing key created in KMS
	// 3. Proper authentication (GOOGLE_APPLICATION_CREDENTIALS)

	t.Skip("Integration test requires GCP KMS setup")
}
