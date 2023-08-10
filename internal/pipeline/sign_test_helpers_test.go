package pipeline

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"testing"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

func newSymmetricKeyPair(t *testing.T, key string, alg jwa.SignatureAlgorithm) (jwk.Key, jwk.Set) {
	t.Helper()

	skey, err := jwk.FromRaw([]byte(key))
	if err != nil {
		t.Fatalf("failed to create symmetric key: %s", err)
	}

	setAll(t, skey, map[string]interface{}{
		jwk.AlgorithmKey: alg,
		jwk.KeyIDKey:     t.Name(),
	})

	set := jwk.NewSet()
	if err := set.AddKey(skey); err != nil {
		t.Fatalf("failed to add key to set: %s", err)
	}

	return skey, set
}

func newRSAKeyPair(t *testing.T, alg jwa.SignatureAlgorithm) (jwk.Key, jwk.Set) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA private key: %s", err)
	}

	return newKeyPair(t, alg, priv)
}

func newECKeyPair(t *testing.T, alg jwa.SignatureAlgorithm, crv elliptic.Curve) (jwk.Key, jwk.Set) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(crv, rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate EC private key: %s", err)
	}

	return newKeyPair(t, alg, priv)
}

func newEdwardsKeyPair(t *testing.T, alg jwa.SignatureAlgorithm) (jwk.Key, jwk.Set) {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate Edwards private key: %s", err)
	}

	return newKeyPair(t, alg, priv)
}

func newKeyPair(t *testing.T, alg jwa.SignatureAlgorithm, privKey any) (jwk.Key, jwk.Set) {
	privJWK, err := jwk.FromRaw(privKey)
	if err != nil {
		t.Fatalf("jwk.FromRaw(%v) error = %v", privKey, err)
	}

	setAll(t, privJWK, map[string]interface{}{
		jwk.AlgorithmKey: alg,
		jwk.KeyIDKey:     t.Name(),
	})

	pubJWK, err := jwk.PublicKeyOf(privJWK)
	if err != nil {
		t.Fatalf("jwk.PublicKeyOf(%v) error = %v", privJWK, err)
	}

	set := jwk.NewSet()
	if err := set.AddKey(pubJWK); err != nil {
		t.Fatalf("failed to add key to set: %s", err)
	}

	return privJWK, set
}

func setAll(t *testing.T, key jwk.Key, values map[string]interface{}) {
	t.Helper()

	for k, v := range values {
		if err := key.Set(k, v); err != nil {
			t.Fatalf("failed to set %s: %s", k, err)
		}
	}
}
