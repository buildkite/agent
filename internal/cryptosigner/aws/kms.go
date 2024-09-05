package awssigner

import (
	"context"
	"crypto"
	"crypto/x509"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/lestrrat-go/jwx/v2/jwa"
)

var (
	ErrInvalidKeyAlgorithm = fmt.Errorf("invalid key algorithm")
	ErrInvalidKeyID        = fmt.Errorf("invalid key ID")
)

// KMS is a crypto.Signer that uses an AWS KMS key for signing.
type KMS struct {
	alg    types.SigningAlgorithmSpec
	jwaAlg jwa.KeyAlgorithm
	client *kms.Client
	kid    string
}

// NewKMS creates a new ECDSA object. This object isnot complete by itself -- it
// needs is setup with the algorithm name to use (see
// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/kms/types#SigningAlgorithmSpec),
// a key ID to use while the AWS SDK makes network
// requests.

// NewKMS creates a new crypto signer which uses AWS KMS to sign data. The keys signing algorithm spec
// dictates the type of signature that will be generated (see
// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/kms/types#SigningAlgorithmSpec),

// The key ID is the unique identifier of the KMS key or key alias.
func NewKMS(client *kms.Client, kmsKeyID string) (*KMS, error) {
	if kmsKeyID == "" {
		return nil, ErrInvalidKeyID
	}

	keyDesc, err := client.GetPublicKey(context.Background(), &kms.GetPublicKeyInput{KeyId: aws.String(kmsKeyID)})
	if err != nil {
		return nil, fmt.Errorf("failed to describe key %q: %w", kmsKeyID, err)
	}

	// the key must be a sign/verify key see https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/kms/types#KeyUsageType
	if keyDesc.KeyUsage != types.KeyUsageTypeSignVerify {
		return nil, fmt.Errorf("invalid key usage. expected SIGN_VERIFY, got %q", keyDesc.KeyUsage)
	}

	// Using the matching KMS keyset as per the following table, we select the
	// appropriate jwa.KeyAlgorithm see https://datatracker.ietf.org/doc/html/rfc7518#section-3.1
	// and https://docs.aws.amazon.com/kms/latest/developerguide/asymmetric-key-specs.html
	//
	// | "alg" Param Value | Digital Signature Algorithm   | KMS KeySpec   |
	// | ----------------- | ----------------------------- | ------------- |
	// | ES256             | ECDSA using P-256 and SHA-256 | ECC_NIST_P256 |
	//
	// We only support ECC_NIST_P256 for now.
	switch keyDesc.KeySpec {
	case types.KeySpecEccNistP256:
		return &KMS{
			client: client,
			kid:    kmsKeyID,
			jwaAlg: jwa.ES256,
			alg:    types.SigningAlgorithmSpecEcdsaSha256,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported key spec: %q, supported key specs are %q", keyDesc.KeySpec,
			[]types.KeySpec{types.KeySpecEccNistP256})
	}
}

// Sign generates a signature from the given digest.
func (sv *KMS) Sign(_ io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if sv.alg == "" {
		return nil, fmt.Errorf("aws.KMS.Sign() requires the types.SigningAlgorithmSpec")
	}
	if sv.kid == "" {
		return nil, fmt.Errorf("aws.KMS.Sign() requires the KMS key ID")
	}

	input := kms.SignInput{
		KeyId:            aws.String(sv.kid),
		Message:          digest,
		MessageType:      types.MessageTypeDigest,
		SigningAlgorithm: sv.alg,
	}
	signed, err := sv.client.Sign(context.Background(), &input)
	if err != nil {
		return nil, fmt.Errorf("failed to sign via KMS: %w", err)
	}

	return signed.Signature, nil
}

// Public returns the corresponding public key.
//
// NOTE: Because the crypto.Signer API does not allow for an error to be returned,
// the return value from this function cannot describe what kind of error
// occurred.
func (sv *KMS) Public() crypto.PublicKey {
	pubkey, _ := sv.GetPublicKey()
	return pubkey
}

// GetPublicKey is an escape hatch for those cases where the user needs
// to debug what went wrong during the GetPublicKey operation.
func (sv *KMS) GetPublicKey() (crypto.PublicKey, error) {
	if sv.kid == "" {
		return nil, fmt.Errorf("aws.KMS.Sign() requires the key ID")
	}

	input := kms.GetPublicKeyInput{
		KeyId: aws.String(sv.kid),
	}

	output, err := sv.client.GetPublicKey(context.Background(), &input)
	if err != nil {
		return nil, fmt.Errorf("failed to get public key from KMS: %w", err)
	}

	if output.KeyUsage != types.KeyUsageTypeSignVerify {
		return nil, fmt.Errorf("invalid key usage. expected SIGN_VERIFY, got %q", output.KeyUsage)
	}

	key, err := x509.ParsePKIXPublicKey(output.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to parse key: %w", err)
	}

	return key, nil
}

// Algorithm returns the equivalent of the KMS key's signing algorithm as a JWA key algorithm.
func (sv *KMS) Algorithm() jwa.KeyAlgorithm {
	return sv.jwaAlg
}
