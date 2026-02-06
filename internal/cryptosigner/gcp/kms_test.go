package gcpsigner

import (
	"context"
	"crypto"
	"testing"

	"github.com/lestrrat-go/jwx/v2/jwa"
)

func TestNewKMS_InvalidKeyResourceName(t *testing.T) {
	ctx := context.Background()

	_, err := NewKMS(ctx, "")
	if err != ErrInvalidKeyResourceName {
		t.Errorf("Expected ErrInvalidKeyResourceName, got %v", err)
	}
}

func TestKMS_HashFunc(t *testing.T) {
	tests := []struct {
		name     string
		hashAlg  crypto.Hash
		expected crypto.Hash
	}{
		{
			name:     "SHA256",
			hashAlg:  crypto.SHA256,
			expected: crypto.SHA256,
		},
		{
			name:     "SHA384",
			hashAlg:  crypto.SHA384,
			expected: crypto.SHA384,
		},
		{
			name:     "SHA512",
			hashAlg:  crypto.SHA512,
			expected: crypto.SHA512,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &KMS{
				hashAlg: tt.hashAlg,
			}

			if got := k.HashFunc(); got != tt.expected {
				t.Errorf("HashFunc() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestKMS_ComputeDigest(t *testing.T) {
	tests := []struct {
		name     string
		hashAlg  crypto.Hash
		data     []byte
		expected int // expected digest length in bytes
	}{
		{
			name:     "SHA256",
			hashAlg:  crypto.SHA256,
			data:     []byte("test data"),
			expected: 32,
		},
		{
			name:     "SHA384",
			hashAlg:  crypto.SHA384,
			data:     []byte("test data"),
			expected: 48,
		},
		{
			name:     "SHA512",
			hashAlg:  crypto.SHA512,
			data:     []byte("test data"),
			expected: 64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &KMS{
				hashAlg: tt.hashAlg,
			}

			digest, err := k.ComputeDigest(tt.data)
			if err != nil {
				t.Fatalf("ComputeDigest() error = %v", err)
			}

			if len(digest) != tt.expected {
				t.Errorf("ComputeDigest() digest length = %d, want %d", len(digest), tt.expected)
			}
		})
	}
}

func TestKMS_ComputeDigest_UnsupportedAlgorithm(t *testing.T) {
	k := &KMS{
		hashAlg: crypto.MD5, // Unsupported
	}

	_, err := k.ComputeDigest([]byte("test data"))
	if err == nil {
		t.Error("Expected error for unsupported hash algorithm, got nil")
	}
}

func TestCRC32C(t *testing.T) {
	// Test with known data
	data := []byte("hello world")
	checksum := crc32c(data)

	// CRC32C should be deterministic
	checksum2 := crc32c(data)
	if checksum != checksum2 {
		t.Errorf("CRC32C is not deterministic: %d != %d", checksum, checksum2)
	}

	// Different data should produce different checksums
	differentData := []byte("goodbye world")
	differentChecksum := crc32c(differentData)
	if checksum == differentChecksum {
		t.Error("Same CRC32C checksum for different data")
	}
}

func TestKMS_Close(t *testing.T) {
	k := &KMS{
		client: nil,
	}

	// Should not panic with nil client
	err := k.Close()
	if err != nil {
		t.Errorf("Close() with nil client returned error: %v", err)
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
