package vaultsigner

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	vault "github.com/hashicorp/vault/api"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

// Signer signs with a pinned Vault Transit key version.
type Signer struct {
	ctx      context.Context
	client   *vault.Logical
	signPath string
	version  int
	public   *ecdsa.PublicKey
	kid      string
}

var pathSegment = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// ValidateKeyReference checks the syntax of a Transit key reference without contacting Vault.
func ValidateKeyReference(ref string) error {
	_, _, _, err := parseKeyReference(ref)
	return err
}

func parseKeyReference(ref string) (string, string, int, error) {
	keyPath, versionText, _ := strings.Cut(ref, "@")
	separator := strings.LastIndex(keyPath, "/keys/")
	version, err := strconv.ParseUint(versionText, 10, 31)
	if separator <= 0 || separator+len("/keys/") == len(keyPath) || err != nil || version == 0 || strings.HasPrefix(versionText, "0") {
		return "", "", 0, fmt.Errorf("expected a Vault Transit key reference in the form <mount>/keys/<name>@<version> with a positive version")
	}
	mount, name := keyPath[:separator], keyPath[separator+len("/keys/"):]
	for segment := range strings.SplitSeq(keyPath, "/") {
		if !pathSegment.MatchString(segment) || segment == "." || segment == ".." {
			return "", "", 0, fmt.Errorf("invalid path segment in Vault Transit key reference")
		}
	}
	if strings.Contains(name, "/") {
		return "", "", 0, fmt.Errorf("the Vault Transit key name must be a single path segment")
	}
	return keyPath, mount + "/sign/" + name, int(version), nil
}

// New reads and caches the public key for a Transit reference of the form
// <mount>/keys/<name>@<version>. The client uses the standard Vault environment.
func New(ctx context.Context, ref string) (*Signer, error) {
	keyPath, signPath, version, err := parseKeyReference(ref)
	if err != nil {
		return nil, err
	}
	client, err := vault.NewClient(nil)
	if err != nil {
		return nil, fmt.Errorf("creating Vault client: %w", err)
	}
	logical := client.Logical()
	metadata, err := logical.ReadWithContext(ctx, keyPath)
	if err != nil {
		return nil, fmt.Errorf("reading Vault Transit key %q: %w", keyPath, err)
	}
	if metadata == nil {
		return nil, fmt.Errorf("no such Vault Transit key %q", keyPath)
	}
	versions, _ := metadata.Data["keys"].(map[string]any)
	entry, _ := versions[strconv.Itoa(version)].(map[string]any)
	publicPEM, _ := entry["public_key"].(string)
	if publicPEM == "" {
		return nil, fmt.Errorf("no public key for Vault Transit key %q version %d", keyPath, version)
	}
	block, _ := pem.Decode([]byte(publicPEM))
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("invalid PEM public key for Vault Transit key %q version %d", keyPath, version)
	}
	raw, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing Vault Transit public key: %w", err)
	}
	public, ok := raw.(*ecdsa.PublicKey)
	if !ok || public.Curve != elliptic.P256() {
		return nil, fmt.Errorf("expected an ECDSA P-256 key for Vault Transit key %q version %d", keyPath, version)
	}
	key, err := jwk.Import(public)
	if err != nil {
		return nil, fmt.Errorf("importing Vault Transit public key as JWK: %w", err)
	}
	thumbprint, err := key.Thumbprint(crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("computing Vault Transit public JWK thumbprint: %w", err)
	}
	return &Signer{
		ctx:      ctx,
		client:   logical,
		signPath: signPath,
		version:  version,
		public:   public,
		kid:      base64.RawURLEncoding.EncodeToString(thumbprint),
	}, nil
}

func (s *Signer) Public() crypto.PublicKey            { return s.public }
func (s *Signer) Algorithm() (jwa.KeyAlgorithm, bool) { return jwa.ES256(), true }
func (s *Signer) KeyID() string                       { return s.kid }

func (s *Signer) Sign(_ io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	if opts == nil || opts.HashFunc() != crypto.SHA256 || len(digest) != crypto.SHA256.Size() {
		return nil, fmt.Errorf("expected a SHA-256 digest for Vault Transit ES256 signing")
	}
	response, err := s.client.WriteWithContext(s.ctx, s.signPath, map[string]any{
		"input":                base64.StdEncoding.EncodeToString(digest),
		"key_version":          s.version,
		"hash_algorithm":       "sha2-256",
		"prehashed":            true,
		"marshaling_algorithm": "asn1",
	})
	if err != nil {
		return nil, fmt.Errorf("signing with Vault Transit: %w", err)
	}
	if response == nil {
		return nil, fmt.Errorf("no signing response from Vault Transit")
	}
	envelope, _ := response.Data["signature"].(string)
	encoded, ok := strings.CutPrefix(envelope, fmt.Sprintf("vault:v%d:", s.version))
	if !ok {
		return nil, fmt.Errorf("the Vault Transit signature is missing or not for key version %d", s.version)
	}
	signature, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || !ecdsa.VerifyASN1(s.public, digest, signature) {
		return nil, fmt.Errorf("invalid Vault Transit signature for key version %d", s.version)
	}
	return signature, nil
}
