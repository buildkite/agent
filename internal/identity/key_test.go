package identity_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/buildkite/agent/v4/internal/identity"
	"go.step.sm/crypto/kms/apiv1"
)

func TestHardwareKeyConfigurationDoesNotFallBack(t *testing.T) {
	for _, uri := range []string{
		"softkms:/tmp/key.pem",
		"mackms:label=agent",
		"mackms:label=agent;se=true;bio=true",
		"capi:key=agent",
		"capi:key=agent;provider=Microsoft Software Key Storage Provider",
		"tpmkms:name=agent",
		"unregistered:label=agent",
	} {
		t.Run(uri, func(t *testing.T) {
			if _, _, err := identity.Open(t.Context(), uri, false); err == nil {
				t.Fatal("accepted a non-hardware or non-persistent key configuration")
			}
		})
	}
}

func TestRegisteredSigningProvider(t *testing.T) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// A provider supplies the existing KeyManager interface, not a new
	// identity-specific plugin API. Its signer need not change proof code.
	apiv1.Register("testsigner", func(context.Context, apiv1.Options) (apiv1.KeyManager, error) {
		return &softwareMacManager{key: private}, nil
	})
	key, closeKey, err := identity.Open(t.Context(), "testsigner:key=agent", false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeKey() //nolint:errcheck // Test result takes precedence.
	if key.Thumbprint() == "" {
		t.Fatal("registered provider did not supply a public identity")
	}
}

func TestAWSKMSConfiguration(t *testing.T) {
	for _, keyURI := range []string{
		"awskms:region=ap-southeast-2",
		"awskms:key-id=alias/agent",
		"awskms:key-id=arn:aws:kms:ap-southeast-2:123456789012:alias/agent",
	} {
		t.Run(keyURI, func(t *testing.T) {
			if _, _, err := identity.Open(t.Context(), keyURI, false); err == nil || !strings.Contains(err.Error(), "immutable key-id") {
				t.Fatalf("Open(%q) error = %v, want immutable key-id requirement", keyURI, err)
			}
		})
	}
	if _, _, err := identity.Open(t.Context(), "awskms:key-id=12345678-1234-1234-1234-123456789012", true); err == nil || !strings.Contains(err.Error(), "provisioned separately") {
		t.Fatalf("AWS key creation error = %v, want external provisioning requirement", err)
	}
}

func TestAWSKMSSignsProofsAndFailsClosed(t *testing.T) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(private.Public())
	if err != nil {
		t.Fatal(err)
	}
	other, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherDER, err := x509.MarshalPKIXPublicKey(other.Public())
	if err != nil {
		t.Fatal(err)
	}
	const keyID = "arn:aws:kms:ap-southeast-2:123456789012:key/12345678-1234-1234-1234-123456789012"
	const otherID = "12345678-1234-1234-1234-123456789013"
	var deny atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		if deny.Load() {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"__type":"AccessDeniedException","message":"signing access revoked"}`))
			return
		}
		var req struct {
			KeyID            string `json:"KeyId"`
			Message          []byte
			MessageType      string
			SigningAlgorithm string
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		switch r.Header.Get("X-Amz-Target") {
		case "TrentService.GetPublicKey":
			pub := der
			if req.KeyID == otherID {
				pub = otherDER
			} else if req.KeyID != keyID {
				t.Errorf("unexpected KMS key: %q", req.KeyID)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"KeyId": req.KeyID, "PublicKey": pub})
		case "TrentService.Sign":
			if req.KeyID != keyID || req.MessageType != "DIGEST" || req.SigningAlgorithm != "ECDSA_SHA_256" || len(req.Message) != sha256.Size {
				t.Errorf("unexpected KMS signing request: %+v", req)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			sig, err := ecdsa.SignASN1(rand.Reader, private, req.Message)
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"KeyId": keyID, "Signature": sig, "SigningAlgorithm": "ECDSA_SHA_256"})
		default:
			t.Errorf("unexpected KMS operation: %s", r.Header.Get("X-Amz-Target"))
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	// Use the real AWS SDK and Smallstep backend with a local KMS protocol
	// server. Never consult the developer's credentials or contact AWS.
	t.Setenv("AWS_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret-key")
	t.Setenv("AWS_SESSION_TOKEN", "test-session-token")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "no-config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "no-credentials"))
	t.Setenv("AWS_ENDPOINT_URL_KMS", server.URL)
	t.Setenv("AWS_IGNORE_CONFIGURED_ENDPOINT_URLS", "false")
	t.Setenv("AWS_MAX_ATTEMPTS", "1")
	keyURI := "awskms:region=ap-southeast-2;key-id=" + keyID
	key, closeKey, err := identity.Open(t.Context(), keyURI, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeKey() //nolint:errcheck // Test result takes precedence.
	target, err := url.Parse("https://agent.example/v3/ping")
	if err != nil {
		t.Fatal(err)
	}
	proof, err := key.Proof(http.MethodPost, target, "session-token", "nonce")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(proof, ".")
	if len(parts) != 3 {
		t.Fatalf("malformed proof: %q", proof)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != 64 {
		t.Fatalf("ES256 signature = %x, %v", sig, err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(&private.PublicKey, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("KMS-backed proof has an invalid ES256 signature")
	}
	if _, _, err := identity.Open(t.Context(), "awskms:region=ap-southeast-2;key-id="+otherID, false); err == nil {
		t.Fatal("accepted a KMS key that cannot sign ES256")
	}
	deny.Store(true)
	if _, err := key.Proof(http.MethodPost, target, "session-token", ""); err == nil {
		t.Fatal("signing succeeded after KMS access was revoked")
	}
	if _, _, err := identity.Open(t.Context(), keyURI, false); err == nil {
		t.Fatal("opened a KMS key after access was revoked")
	}
}

func TestCAPICertificateLookupIsRejectedBeforeOpeningBackend(t *testing.T) {
	// A certificate's private key can belong to a software provider even
	// when the URI selects the TPM provider. Reject the configuration before
	// attempting to open CAPI, including on systems without Windows support.
	const uri = "capi:provider=Microsoft Platform Crypto Provider;sha1=0123456789abcdef0123456789abcdef01234567"
	_, _, err := identity.Open(t.Context(), uri, false)
	if err == nil || !strings.Contains(err.Error(), "explicit key container") {
		t.Fatalf("certificate-only identity error = %v, want explicit key container requirement", err)
	}
}

func TestOpenRejectsUnverifiedMacKey(t *testing.T) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// Model MacKMS returning a software key despite se=true. The identity
	// layer must independently verify its protection before accepting it.
	original, ok := apiv1.LoadKeyManagerNewFunc(apiv1.MacKMS)
	if !ok {
		t.Fatal("MacKMS backend is not registered")
	}
	t.Cleanup(func() { apiv1.Register(apiv1.MacKMS, original) })
	apiv1.Register(apiv1.MacKMS, func(context.Context, apiv1.Options) (apiv1.KeyManager, error) {
		return &softwareMacManager{key: private}, nil
	})
	_, closeKey, err := identity.Open(t.Context(), "mackms:label=agent;se=true", false)
	if closeKey != nil {
		_ = closeKey()
	}
	if err == nil || !strings.Contains(err.Error(), "verify Secure Enclave") {
		t.Fatalf("unverified software key error = %v, want Secure Enclave verification failure", err)
	}
}

type softwareMacManager struct {
	apiv1.KeyManager
	key *ecdsa.PrivateKey
}

func (m *softwareMacManager) GetPublicKey(*apiv1.GetPublicKeyRequest) (crypto.PublicKey, error) {
	return m.key.Public(), nil
}

func (m *softwareMacManager) CreateSigner(*apiv1.CreateSignerRequest) (crypto.Signer, error) {
	return m.key, nil
}

func (m *softwareMacManager) Close() error { return nil }

func TestPublicIdentityContainsNoPrivateKey(t *testing.T) {
	for _, curve := range []elliptic.Curve{elliptic.P256(), elliptic.P384()} {
		private, err := ecdsa.GenerateKey(curve, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		key, err := identity.New(private)
		if curve != elliptic.P256() {
			if err == nil {
				t.Fatal("accepted a key that cannot sign ES256")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(key)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `"d":`) || !strings.Contains(string(data), `"jkt":`) {
			t.Fatalf("identity is not public enrollment metadata: %s", data)
		}
	}
}
