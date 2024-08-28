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

type ECDSA struct {
	alg    types.SigningAlgorithmSpec
	jwaAlg jwa.KeyAlgorithm
	client *kms.Client
	kid    string
}

// NewECDSA creates a new ECDSA object. This object isnot complete by itself -- it
// needs to be setup with the algorithm name to use (see
// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/kms/types#SigningAlgorithmSpec),
// a key ID to use while the AWS SDK makes network
// requests.
func NewECDSA(client *kms.Client) *ECDSA {
	return &ECDSA{
		client: client,
	}
}

// Sign generates a signature from the given digest.
func (sv *ECDSA) Sign(_ io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if sv.alg == "" {
		return nil, fmt.Errorf(`aws.ECDSA.Sign() requires the types.SigningAlgorithmSpec`)
	}
	if sv.kid == "" {
		return nil, fmt.Errorf(`aws.ECDSA.Sign() requires the KMS key ID`)
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
func (sv *ECDSA) Public() crypto.PublicKey {
	pubkey, _ := sv.GetPublicKey()
	return pubkey
}

// This method is an escape hatch for those cases where the user needs
// to debug what went wrong during the GetPublicKey operation.
func (sv *ECDSA) GetPublicKey() (crypto.PublicKey, error) {
	if sv.kid == "" {
		return nil, fmt.Errorf(`aws.ECDSA.Sign() requires the key ID`)
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
func (sv *ECDSA) Algorithm() jwa.KeyAlgorithm {
	return sv.jwaAlg
}

// WithAlgorithm associates a new types.SigningAlgorithmSpec with the object, which will be used for Sign() and Public()
func (cs *ECDSA) WithAlgorithm(v types.SigningAlgorithmSpec) *ECDSA {
	return &ECDSA{
		client: cs.client,
		alg:    v,
		kid:    cs.kid,
		jwaAlg: cs.jwaAlg,
	}
}

// WithKeyID associates a new string with the object, which will be used for Sign() and Public()
func (cs *ECDSA) WithKeyID(v string) *ECDSA {
	return &ECDSA{
		client: cs.client,
		alg:    cs.alg,
		kid:    v,
		jwaAlg: cs.jwaAlg,
	}
}

func (cs *ECDSA) WithJWAKeyAlgorithm(jwaAlg jwa.KeyAlgorithm) *ECDSA {
	return &ECDSA{
		client: cs.client,
		alg:    cs.alg,
		kid:    cs.kid,
		jwaAlg: jwaAlg,
	}
}
