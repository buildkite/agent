package jwkutil

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

func newRSAJWK(t *testing.T) jwk.Key {
	t.Helper()

	privRSA, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey(rand.Reader, 2048) error = %v", err)
	}

	key, err := jwk.FromRaw(privRSA)
	if err != nil {
		t.Fatalf("jwk.FromRaw(privRSA) error = %v", err)
	}

	return key
}

func newECJWK(t *testing.T) jwk.Key {
	t.Helper()

	privEC, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey(elliptic.P256(), rand.Reader) error = %v", err)
	}

	key, err := jwk.FromRaw(privEC)
	if err != nil {
		t.Fatalf("jwk.FromRaw(privEC) error = %v", err)
	}

	return key
}

func newOKPJWK(t *testing.T) jwk.Key {
	t.Helper()

	_, privOKP, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey(rand.Reader) error = %v", err)
	}

	key, err := jwk.FromRaw(privOKP)
	if err != nil {
		t.Fatalf("jwk.FromRaw(privOKP) error = %v", err)
	}

	return key
}

func newOctetSeqJWK(t *testing.T) jwk.Key {
	t.Helper()

	payload := make([]byte, 32)
	_, err := rand.Read(payload)
	if err != nil {
		t.Fatalf("rand.Read(key) error = %v", err)
	}

	key, err := jwk.FromRaw(payload)
	if err != nil {
		t.Fatalf("jwk.FromRaw(payload) error = %v", err)
	}

	return key
}

func keyPS256(t *testing.T) jwk.Key {
	t.Helper()

	key := newRSAJWK(t)

	err := key.Set(jwk.AlgorithmKey, jwa.PS256)
	if err != nil {
		t.Fatalf("keyWithAlg.Set(%v, %v) error = %v", jwk.AlgorithmKey, jwa.RS256, err)
	}

	return key
}

func encryptionKey(t *testing.T) jwk.Key {
	t.Helper()

	key := newRSAJWK(t)

	err := key.Set(jwk.AlgorithmKey, jwa.RSA_OAEP)
	if err != nil {
		t.Fatalf("encryptionKey.Set(%v, %v) error = %v", jwk.AlgorithmKey, jwa.RSA_OAEP, err)
	}

	return key
}
