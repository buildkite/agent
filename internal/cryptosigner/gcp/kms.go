package gcpsigner

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"hash"
	"hash/crc32"
	"io"

	kms "cloud.google.com/go/kms/apiv1"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/wrapperspb"

	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
)

var (
	ErrInvalidKeyResourceName = fmt.Errorf("invalid GCP KMS key resource name")
	ErrInvalidKeyAlgorithm    = fmt.Errorf("unsupported key algorithm")
	ErrInvalidKeyPurpose      = fmt.Errorf("key must have ASYMMETRIC_SIGN purpose")
	ErrChecksumMismatch       = fmt.Errorf("signature checksum verification failed")
	ErrUnsupportedHashAlg     = fmt.Errorf("unsupported hash algorithm")
)

// KMS is a crypto.Signer that uses a GCP KMS key for signing.
type KMS struct {
	ctx     context.Context
	client  *kms.KeyManagementClient
	kid     string // Full KMS key resource name
	jwaAlg  jwa.KeyAlgorithm
	alg     kmspb.CryptoKeyVersion_CryptoKeyVersionAlgorithm
	hashAlg crypto.Hash
}

// NewKMS creates a new crypto signer which uses GCP KMS to sign data.
// The keyResourceName must be in the format:
// projects/{project}/locations/{location}/keyRings/{keyring}/cryptoKeys/{key}/cryptoKeyVersions/{version}
// Additional client options can be provided via opts. If no options are provided,
// GCP will automatically use Application Default Credentials (ADC).
func NewKMS(ctx context.Context, keyResourceName string, opts ...option.ClientOption) (*KMS, error) {
	if keyResourceName == "" {
		return nil, ErrInvalidKeyResourceName
	}

	client, err := kms.NewKeyManagementClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create KMS client: %w", err)
	}

	// Get the public key to determine the algorithm and validate the key
	pubKeyResp, err := client.GetPublicKey(ctx, &kmspb.GetPublicKeyRequest{
		Name: keyResourceName,
	})
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to get public key for %q: %w", keyResourceName, err)
	}

	// Validate key purpose
	if pubKeyResp.ProtectionLevel == kmspb.ProtectionLevel_EXTERNAL {
		// For external keys, we can't validate the purpose as easily
		// so we'll trust the configuration
	}

	// Map GCP algorithm to JWA algorithm
	// Based on https://cloud.google.com/kms/docs/algorithms
	var jwaAlg jwa.KeyAlgorithm
	var hashAlg crypto.Hash

	switch pubKeyResp.Algorithm {
	case kmspb.CryptoKeyVersion_EC_SIGN_P256_SHA256:
		jwaAlg = jwa.ES256
		hashAlg = crypto.SHA256
	case kmspb.CryptoKeyVersion_EC_SIGN_P384_SHA384:
		jwaAlg = jwa.ES384
		hashAlg = crypto.SHA384
	case kmspb.CryptoKeyVersion_RSA_SIGN_PKCS1_2048_SHA256,
		kmspb.CryptoKeyVersion_RSA_SIGN_PKCS1_3072_SHA256,
		kmspb.CryptoKeyVersion_RSA_SIGN_PKCS1_4096_SHA256:
		jwaAlg = jwa.RS256
		hashAlg = crypto.SHA256
	case kmspb.CryptoKeyVersion_RSA_SIGN_PSS_2048_SHA256,
		kmspb.CryptoKeyVersion_RSA_SIGN_PSS_3072_SHA256,
		kmspb.CryptoKeyVersion_RSA_SIGN_PSS_4096_SHA256:
		jwaAlg = jwa.PS256
		hashAlg = crypto.SHA256
	case kmspb.CryptoKeyVersion_RSA_SIGN_PKCS1_4096_SHA512:
		jwaAlg = jwa.RS512
		hashAlg = crypto.SHA512
	case kmspb.CryptoKeyVersion_RSA_SIGN_PSS_4096_SHA512:
		jwaAlg = jwa.PS512
		hashAlg = crypto.SHA512
	default:
		client.Close()
		return nil, fmt.Errorf("%w: %s (supported: EC_SIGN_P256_SHA256, EC_SIGN_P384_SHA384, RSA_SIGN_*)",
			ErrInvalidKeyAlgorithm, pubKeyResp.Algorithm)
	}

	return &KMS{
		ctx:     ctx,
		client:  client,
		kid:     keyResourceName,
		jwaAlg:  jwaAlg,
		alg:     pubKeyResp.Algorithm,
		hashAlg: hashAlg,
	}, nil
}

// Sign generates a signature from the given digest using GCP KMS.
func (k *KMS) Sign(_ io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if k.kid == "" {
		return nil, fmt.Errorf("gcp.KMS.Sign() requires a key resource name")
	}

	// Calculate CRC32C checksum of the digest for integrity verification
	digestCRC32C := crc32c(digest)

	// Determine which hash algorithm was used based on the digest size and opts
	var digestProto *kmspb.Digest
	if opts != nil {
		switch opts.HashFunc() {
		case crypto.SHA256:
			digestProto = &kmspb.Digest{
				Digest: &kmspb.Digest_Sha256{Sha256: digest},
			}
		case crypto.SHA384:
			digestProto = &kmspb.Digest{
				Digest: &kmspb.Digest_Sha384{Sha384: digest},
			}
		case crypto.SHA512:
			digestProto = &kmspb.Digest{
				Digest: &kmspb.Digest_Sha512{Sha512: digest},
			}
		default:
			return nil, fmt.Errorf("unsupported hash function: %v", opts.HashFunc())
		}
	} else {
		// Default to the key's configured hash algorithm
		switch k.hashAlg {
		case crypto.SHA256:
			digestProto = &kmspb.Digest{
				Digest: &kmspb.Digest_Sha256{Sha256: digest},
			}
		case crypto.SHA384:
			digestProto = &kmspb.Digest{
				Digest: &kmspb.Digest_Sha384{Sha384: digest},
			}
		case crypto.SHA512:
			digestProto = &kmspb.Digest{
				Digest: &kmspb.Digest_Sha512{Sha512: digest},
			}
		default:
			return nil, fmt.Errorf("unknown hash algorithm for key")
		}
	}

	// Create the signing request
	req := &kmspb.AsymmetricSignRequest{
		Name:         k.kid,
		Digest:       digestProto,
		DigestCrc32C: wrapperspb.Int64(int64(digestCRC32C)),
	}

	// Call GCP KMS to sign
	resp, err := k.client.AsymmetricSign(k.ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to sign via GCP KMS: %w", err)
	}

	// Verify the response integrity using CRC32C
	if !resp.VerifiedDigestCrc32C {
		return nil, fmt.Errorf("%w: digest CRC32C not verified", ErrChecksumMismatch)
	}

	signatureCRC32C := crc32c(resp.Signature)
	if int64(signatureCRC32C) != resp.SignatureCrc32C.Value {
		return nil, fmt.Errorf("%w: signature CRC32C mismatch", ErrChecksumMismatch)
	}

	return resp.Signature, nil
}

// Public returns the corresponding public key.
//
// NOTE: Because the crypto.Signer API does not allow for an error to be returned,
// the return value from this function cannot describe what kind of error occurred.
func (k *KMS) Public() crypto.PublicKey {
	pubkey, _ := k.GetPublicKey()
	return pubkey
}

// GetPublicKey retrieves the public key from GCP KMS.
func (k *KMS) GetPublicKey() (crypto.PublicKey, error) {
	if k.kid == "" {
		return nil, fmt.Errorf("gcp.KMS.GetPublicKey() requires a key resource name")
	}

	req := &kmspb.GetPublicKeyRequest{
		Name: k.kid,
	}

	resp, err := k.client.GetPublicKey(k.ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get public key from GCP KMS: %w", err)
	}

	// Parse the PEM-encoded public key
	block, _ := pem.Decode([]byte(resp.Pem))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM public key")
	}

	// Parse the public key
	pubkey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	// Validate the key type matches the algorithm
	switch k.alg {
	case kmspb.CryptoKeyVersion_EC_SIGN_P256_SHA256,
		kmspb.CryptoKeyVersion_EC_SIGN_P384_SHA384:
		if _, ok := pubkey.(*ecdsa.PublicKey); !ok {
			return nil, fmt.Errorf("expected ECDSA public key, got %T", pubkey)
		}
	case kmspb.CryptoKeyVersion_RSA_SIGN_PKCS1_2048_SHA256,
		kmspb.CryptoKeyVersion_RSA_SIGN_PKCS1_3072_SHA256,
		kmspb.CryptoKeyVersion_RSA_SIGN_PKCS1_4096_SHA256,
		kmspb.CryptoKeyVersion_RSA_SIGN_PKCS1_4096_SHA512,
		kmspb.CryptoKeyVersion_RSA_SIGN_PSS_2048_SHA256,
		kmspb.CryptoKeyVersion_RSA_SIGN_PSS_3072_SHA256,
		kmspb.CryptoKeyVersion_RSA_SIGN_PSS_4096_SHA256,
		kmspb.CryptoKeyVersion_RSA_SIGN_PSS_4096_SHA512:
		if _, ok := pubkey.(*rsa.PublicKey); !ok {
			return nil, fmt.Errorf("expected RSA public key, got %T", pubkey)
		}
	}

	return pubkey, nil
}

// Algorithm returns the JWA key algorithm for this key.
func (k *KMS) Algorithm() jwa.KeyAlgorithm {
	return k.jwaAlg
}

// Close closes the underlying KMS client connection.
func (k *KMS) Close() error {
	if k.client != nil {
		return k.client.Close()
	}
	return nil
}

// crc32c computes the CRC32C checksum using the Castagnoli polynomial.
func crc32c(data []byte) uint32 {
	t := crc32.MakeTable(crc32.Castagnoli)
	return crc32.Checksum(data, t)
}

// HashFunc returns the hash function to use for this key's algorithm.
func (k *KMS) HashFunc() crypto.Hash {
	return k.hashAlg
}

// ComputeDigest computes the digest of the data using the appropriate hash function.
func (k *KMS) ComputeDigest(data []byte) ([]byte, error) {
	var h hash.Hash
	switch k.hashAlg {
	case crypto.SHA256:
		h = sha256.New()
	case crypto.SHA384:
		h = sha512.New384()
	case crypto.SHA512:
		h = sha512.New()
	default:
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedHashAlg, k.hashAlg)
	}
	h.Write(data)
	return h.Sum(nil), nil
}
