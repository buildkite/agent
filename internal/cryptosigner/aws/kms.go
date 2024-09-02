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

	// there should be at least one signing algorithm available, and for sign/verify keys there should only be one.
	if len(keyDesc.SigningAlgorithms) != 1 {
		return nil, fmt.Errorf("expected one signing algorithm for key %q got %q", kmsKeyID, keyDesc.SigningAlgorithms)
	}

	alg := keyDesc.SigningAlgorithms[0]

	// Using the matching KMS keyset as per the following table, we select the
	// appropriate jwa.KeyAlgorithm see https://datatracker.ietf.org/doc/html/rfc7518#section-3.1
	// and https://docs.aws.amazon.com/kms/latest/developerguide/asymmetric-key-specs.html
	//
	// | "alg" Param Value | Digital Signature Algorithm | KMS KeySpec |
	// | ----------------- | --------------------------- | ----------- |
	// | ES256   | ECDSA using P-256 and SHA-256   | ECC_NIST_P256 |
	// | ES384   | ECDSA using P-384 and SHA-384   | ECC_NIST_P384 |
	// | ES512   | ECDSA using P-521 and SHA-512   | ECC_NIST_P521 |
	// | RS256   | RSASSA-PKCS1-v1_5 using SHA-256 | RSASSA_PKCS1_V1_5_SHA_256 |
	// | RS384   | RSASSA-PKCS1-v1_5 using SHA-384 | RSASSA_PKCS1_V1_5_SHA_384 |
	// | RS512   | RSASSA-PKCS1-v1_5 using SHA-512 | RSASSA_PKCS1_V1_5_SHA_512 |
	//
	var jwaAlg jwa.KeyAlgorithm
	switch alg {
	case types.SigningAlgorithmSpecEcdsaSha256:
		jwaAlg = jwa.ES256
	case types.SigningAlgorithmSpecEcdsaSha384:
		jwaAlg = jwa.ES384
	case types.SigningAlgorithmSpecEcdsaSha512:
		jwaAlg = jwa.ES512
	case types.SigningAlgorithmSpecRsassaPkcs1V15Sha256:
		jwaAlg = jwa.RS256
	case types.SigningAlgorithmSpecRsassaPkcs1V15Sha384:
		jwaAlg = jwa.RS384
	case types.SigningAlgorithmSpecRsassaPkcs1V15Sha512:
		jwaAlg = jwa.RS512
	default:
		return nil, fmt.Errorf("unsupported signing algorithm %q", alg)
	}

	return &KMS{
		client: client,
		jwaAlg: jwaAlg,
		alg:    alg,
		kid:    kmsKeyID,
	}, nil
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
