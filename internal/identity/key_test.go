package identity_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/internal/identity"
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
