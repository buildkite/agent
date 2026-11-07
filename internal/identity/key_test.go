package identity_test

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"strings"
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
	} {
		t.Run(uri, func(t *testing.T) {
			if _, _, err := identity.Open(t.Context(), uri, false); err == nil {
				t.Fatal("accepted a non-hardware or non-persistent key configuration")
			}
		})
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
