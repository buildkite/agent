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
	ErrInvalidKeyAlgorithm = fmt.Errorf(`invalid key algorithm`)
	ErrInvalidKeyID        = fmt.Errorf(`invalid key ID`)
)

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
func NewKMS(client *kms.Client, kmsKeyID string) (*KMS, error) {
	if kmsKeyID == "" {
		return nil, ErrInvalidKeyID
	}

	keyDesc, err := client.GetPublicKey(context.Background(), &kms.GetPublicKeyInput{KeyId: aws.String(kmsKeyID)})
	if err != nil {
		return nil, fmt.Errorf(`failed to describe key %q: %w`, kmsKeyID, err)
	}

	if keyDesc.KeyUsage != types.KeyUsageTypeSignVerify {
		return nil, fmt.Errorf(`invalid key usage. expected SIGN_VERIFY, got %q`, keyDesc.KeyUsage)
	}

	if len(keyDesc.SigningAlgorithms) == 0 {
		return nil, fmt.Errorf(`no signing algorithms found for key %q`, kmsKeyID)
	}

	alg := keyDesc.SigningAlgorithms[0]

	// using the first algorithm in the list we select the appropriate jwa.KeyAlgorithm
	var jwaAlg jwa.KeyAlgorithm
	switch alg {
	case types.SigningAlgorithmSpecEcdsaSha256:
		jwaAlg = jwa.ES256
	case types.SigningAlgorithmSpecRsassaPkcs1V15Sha256:
		jwaAlg = jwa.RS256
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
		return nil, fmt.Errorf(`aws.KMS.Sign() requires the types.SigningAlgorithmSpec`)
	}
	if sv.kid == "" {
		return nil, fmt.Errorf(`aws.KMS.Sign() requires the KMS key ID`)
	}

	input := kms.SignInput{
		KeyId:            aws.String(sv.kid),
		Message:          digest,
		MessageType:      types.MessageTypeDigest,
		SigningAlgorithm: sv.alg,
	}
	signed, err := sv.client.Sign(context.Background(), &input)
	if err != nil {
		return nil, fmt.Errorf(`failed to sign via KMS: %w`, err)
	}

	return signed.Signature, nil
}

// Public returns the corresponding public key.
//
// Because the crypto.Signer API does not allow for an error to be returned,
// the return value from this function cannot describe what kind of error
// occurred.
func (sv *KMS) Public() crypto.PublicKey {
	pubkey, _ := sv.GetPublicKey()
	return pubkey
}

// This method is an escape hatch for those cases where the user needs
// to debug what went wrong during the GetPublicKey operation.
func (sv *KMS) GetPublicKey() (crypto.PublicKey, error) {
	if sv.kid == "" {
		return nil, fmt.Errorf(`aws.KMS.Sign() requires the key ID`)
	}

	input := kms.GetPublicKeyInput{
		KeyId: aws.String(sv.kid),
	}

	output, err := sv.client.GetPublicKey(context.Background(), &input)
	if err != nil {
		return nil, fmt.Errorf(`failed to get public key from KMS: %w`, err)
	}

	if output.KeyUsage != types.KeyUsageTypeSignVerify {
		return nil, fmt.Errorf(`invalid key usage. expected SIGN_VERIFY, got %q`, output.KeyUsage)
	}

	key, err := x509.ParsePKIXPublicKey(output.PublicKey)
	if err != nil {
		return nil, fmt.Errorf(`failed to parse key: %w`, err)
	}

	return key, nil
}

// jwa.ES256
func (sv *KMS) Algorithm() jwa.KeyAlgorithm {
	return sv.jwaAlg
}
