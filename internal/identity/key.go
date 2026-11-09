// Package identity manages host signing keys for experimental key-bound agent sessions.
package identity

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
	"go.step.sm/crypto/kms"
	"go.step.sm/crypto/kms/apiv1"
	_ "go.step.sm/crypto/kms/awskms" // AWS KMS signing keys.
	_ "go.step.sm/crypto/kms/capi"   // Windows TPM-backed CNG keys.
	_ "go.step.sm/crypto/kms/mackms" // macOS Secure Enclave keys.
	_ "go.step.sm/crypto/kms/tpmkms" // TPM 2.0 keys.
	"go.step.sm/crypto/kms/uri"
)

// Key signs proofs without exposing the private key to callers.
type Key struct {
	signer crypto.Signer
	jwk    jwk.Key
	jkt    string
}

// New wraps a P-256 signer. It makes no assertion about the signer's protection
// level; production callers should obtain their signer through Open.
func New(signer crypto.Signer) (*Key, error) {
	pub, ok := signer.Public().(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		return nil, fmt.Errorf("host identity requires an ECDSA P-256 key")
	}
	publicJWK, err := jwk.Import(pub)
	if err != nil {
		return nil, fmt.Errorf("import host public key: %w", err)
	}
	thumbprint, err := publicJWK.Thumbprint(crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("thumbprint host public key: %w", err)
	}
	return &Key{signer: signer, jwk: publicJWK, jkt: base64.RawURLEncoding.EncodeToString(thumbprint)}, nil
}

// Open opens a key through its registered KMS provider, or creates a local
// hardware key when create is explicitly requested. AWS keys must be provisioned
// separately. Providers are registered through Smallstep's apiv1.Register and
// must enforce their key protection requirements; software fallback is disabled.
// The returned close function releases the manager, not the persistent key.
// Missing keys are never silently replaced.
func Open(ctx context.Context, keyURI string, create bool) (*Key, func() error, error) {
	if create && uri.HasScheme("awskms", keyURI) {
		return nil, nil, fmt.Errorf("AWS KMS keys must be provisioned separately; omit --create to open an existing key")
	}
	km, err := manager(ctx, keyURI)
	if err != nil {
		return nil, nil, err
	}
	req := apiv1.CreateSignerRequest{SigningKey: keyURI}
	if create {
		if _, err := km.GetPublicKey(&apiv1.GetPublicKeyRequest{Name: keyURI}); err == nil {
			_ = km.Close()
			return nil, nil, fmt.Errorf("host key already exists; omit --create to inspect it")
		}
		created, err := km.CreateKey(&apiv1.CreateKeyRequest{
			Name: keyURI, SignatureAlgorithm: apiv1.ECDSAWithSHA256,
		})
		if err != nil {
			_ = km.Close()
			return nil, nil, fmt.Errorf("create host key: %w", err)
		}
		req = created.CreateSignerRequest
	}
	if uri.HasScheme("mackms", keyURI) {
		publicKey, err := km.GetPublicKey(&apiv1.GetPublicKeyRequest{Name: req.SigningKey})
		if err != nil {
			_ = km.Close()
			return nil, nil, fmt.Errorf("read host public key: %w", err)
		}
		req.SigningKey, err = secureEnclaveKeyURI(req.SigningKey, publicKey)
		if err != nil {
			_ = km.Close()
			return nil, nil, fmt.Errorf("verify Secure Enclave host key: %w", err)
		}
	}
	signer, err := km.CreateSigner(&req)
	if err != nil {
		_ = km.Close()
		return nil, nil, fmt.Errorf("open host key: %w", err)
	}
	key, err := New(signer)
	if err != nil {
		_ = km.Close()
		return nil, nil, err
	}
	return key, km.Close, nil
}

func manager(ctx context.Context, keyURI string) (kms.KeyManager, error) {
	u, err := uri.Parse(keyURI)
	if err != nil {
		return nil, fmt.Errorf("parse host key URI: %w", err)
	}
	switch u.Scheme {
	case "mackms":
		if !u.GetBool("se") || u.GetBool("bio") {
			return nil, fmt.Errorf("host keys require mackms se=true and bio=false for unattended signing")
		}
	case "capi":
		if u.Get("provider") != "Microsoft Platform Crypto Provider" {
			return nil, fmt.Errorf("host keys require the Microsoft Platform Crypto Provider")
		}
		// Certificate lookup can acquire a key from a different provider.
		// A named container keeps lookup on the explicitly selected TPM.
		if u.Get("key") == "" {
			return nil, fmt.Errorf("host CAPI keys require an explicit key container (key=...); certificate lookup is not supported")
		}
	case "tpmkms":
		if u.Get("storage-directory") == "" {
			return nil, fmt.Errorf("host TPM keys require an explicit storage-directory")
		}
	case "awskms":
		keyID := strings.ToLower(u.Get("key-id"))
		if keyID == "" || strings.HasPrefix(keyID, "alias/") || strings.Contains(keyID, ":alias/") {
			return nil, fmt.Errorf("AWS KMS identity requires an immutable key-id or key ARN, not an alias")
		}
	case "softkms":
		return nil, fmt.Errorf("software host keys are not supported")
	}
	// KMS provider factories are registered by linked packages. Keep dispatch
	// here generic so a provider can be added without changing the proof code.
	return kms.New(ctx, apiv1.Options{URI: keyURI})
}

// Thumbprint is the RFC 7638 SHA-256 public-key thumbprint.
func (k *Key) Thumbprint() string { return k.jkt }

// MarshalJSON exposes only the public key and its thumbprint, for enrollment.
func (k *Key) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		JWK jwk.Key `json:"jwk"`
		JKT string  `json:"jkt"`
	}{k.jwk, k.jkt})
}

// Proof produces an RFC 9449 ES256 proof. Pass an empty accessToken for the
// enrollment exchange; subsequent requests must bind the proof to their token.
func (k *Key) Proof(method string, target *url.URL, accessToken, nonce string) (string, error) {
	u := *target
	u.RawQuery, u.ForceQuery, u.Fragment, u.RawFragment = "", false, "", ""
	claims := struct {
		JTI   string `json:"jti"`
		HTM   string `json:"htm"`
		HTU   string `json:"htu"`
		IAT   int64  `json:"iat"`
		ATH   string `json:"ath,omitempty"`
		Nonce string `json:"nonce,omitempty"`
	}{JTI: uuid.NewString(), HTM: method, HTU: u.String(), IAT: time.Now().Unix(), Nonce: nonce}
	if accessToken != "" {
		hash := sha256.Sum256([]byte(accessToken))
		claims.ATH = base64.RawURLEncoding.EncodeToString(hash[:])
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	headers := jws.NewHeaders()
	if err := headers.Set(jws.TypeKey, "dpop+jwt"); err != nil {
		return "", err
	}
	if err := headers.Set(jws.JWKKey, k.jwk); err != nil {
		return "", err
	}
	proof, err := jws.Sign(payload, jws.WithKey(jwa.ES256(), k.signer, jws.WithProtectedHeaders(headers)))
	if err != nil {
		return "", fmt.Errorf("sign host proof: %w", err)
	}
	return string(proof), nil
}
